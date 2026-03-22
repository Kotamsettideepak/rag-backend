package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"gin-backend/ingest"
	"gin-backend/llm"
	"gin-backend/store"
	"gin-backend/trace"

	"github.com/gin-gonic/gin"
)

type ChatRequest struct {
	ChatID  string `json:"chat_id"`
	Message string `json:"message"`
}

type createChatRequest struct {
	Title string `json:"title"`
}

func ChatHandler(c *gin.Context) {
	trace.Start("CHAT", c.Request.URL.Path)
	log.Printf("[chat] request received: method=%s path=%s", c.Request.Method, c.Request.URL.Path)

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	req.ChatID = strings.TrimSpace(req.ChatID)
	req.Message = strings.TrimSpace(req.Message)
	if req.ChatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	if _, err := store.DefaultStore().GetChat(c.Request.Context(), req.ChatID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}
	log.Printf("[chat] chat_id=%s question=%s", req.ChatID, previewText(req.Message, 220))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	if _, err := store.DefaultStore().SaveMessage(ctx, req.ChatID, "user", req.Message); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save user message"})
		return
	}

	contextResult, err := ingest.DefaultManager().SearchContext(ctx, req.Message, req.ChatID, user.ID)
	if err != nil {
		log.Printf("[chat] failed to search vector context: %v", err)
		trace.End("CHAT", "failed during retrieval")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search for context: " + err.Error()})
		return
	}

	previousMessages, err := store.DefaultStore().ListMessages(ctx, req.ChatID, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load message history"})
		return
	}

	log.Printf("[chat] context modality=%s", contextResult.Modality)
	log.Printf("[chat] context preview=%s", previewText(contextResult.Context, 320))
	prompt := buildPrompt(contextResult.Modality, contextResult.Context, buildConversationContext(previousMessages), req.Message)
	log.Printf("[chat] sending to llm provider=Groq model=%s prompt_preview=%s", llm.CurrentGroqModel(), previewText(prompt, 420))

	client := llm.NewGroqClient()
	answer, err := client.GenerateResponse([]llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Printf("[chat] failed to generate answer: %v", err)
		trace.End("CHAT", "failed during groq generation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate text response"})
		return
	}
	trace.End("CHAT", "answer generated")

	if _, err := store.DefaultStore().SaveMessage(ctx, req.ChatID, "assistant", answer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save assistant message"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"answer": answer,
	})
}

func CreateChatHandler(c *gin.Context) {
	var req createChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}

	chat, err := store.DefaultStore().CreateChat(c.Request.Context(), user.ID, strings.TrimSpace(req.Title))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create chat"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"chat_id": chat.ID})
}

func ListChatsHandler(c *gin.Context) {
	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}

	chats, err := store.DefaultStore().ListChats(c.Request.Context(), user.ID, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load chats"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"chats": chats})
}

func ChatMessagesHandler(c *gin.Context) {
	chatID := strings.TrimSpace(c.Param("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	if _, err := store.DefaultStore().GetChat(c.Request.Context(), chatID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}

	messages, err := store.DefaultStore().ListMessages(c.Request.Context(), chatID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load messages"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

func ChatUploadsHandler(c *gin.Context) {
	chatID := strings.TrimSpace(c.Param("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	if _, err := store.DefaultStore().GetChat(c.Request.Context(), chatID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}

	uploads, err := store.DefaultStore().ListUserUploadedData(c.Request.Context(), chatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load uploads"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"uploads": uploads})
}

func DeleteChatHandler(c *gin.Context) {
	chatID := strings.TrimSpace(c.Param("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}

	if _, err := store.DefaultStore().GetChat(c.Request.Context(), chatID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}

	if err := ingest.DefaultManager().DeleteChatContext(chatID, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete chat context"})
		return
	}

	if err := store.DefaultStore().DeleteChat(c.Request.Context(), chatID, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete chat"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Chat deleted successfully"})
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func buildConversationContext(messages []store.Message) string {
	if len(messages) == 0 {
		return ""
	}

	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}

		lines = append(lines, fmt.Sprintf("%s: %s", strings.ToUpper(strings.TrimSpace(message.Role)), content))
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
