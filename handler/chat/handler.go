package chat

import (
	"log"
	"net/http"
	"strings"

	"gin-backend/middleware"
	chatservice "gin-backend/service/chat"
	chatserviceprompt "gin-backend/service/chat/prompt"

	"github.com/gin-gonic/gin"
)

// ChatHandler handles the POST /chat endpoint (non-streaming).
func ChatHandler(c *gin.Context) {
	var req struct {
		ChatID         string                            `json:"chat_id"`
		Message        string                            `json:"message"`
		RecentMessages []chatserviceprompt.HistoryMessage `json:"recent_messages"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.Message = strings.TrimSpace(req.Message)
	if req.ChatID == "" || req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id and message are required"})
		return
	}

	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}

	answer, err := chatservice.Default().Answer(c.Request.Context(), user.ID, req.ChatID, req.Message)
	if err != nil {
		log.Printf("[chat] answer failed: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"answer": answer})
}

// CreateChatHandler handles POST /chat/create.
func CreateChatHandler(c *gin.Context) {
	var req struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	chat, err := chatservice.Default().Create(c.Request.Context(), user.ID, strings.TrimSpace(req.Title))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create chat"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"chat_id": chat.ID})
}

// ListChatsHandler handles GET /chat/list.
func ListChatsHandler(c *gin.Context) {
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	chats, err := chatservice.Default().List(c.Request.Context(), user.ID, 10)
	if err != nil {
		log.Printf("[chat] list failed user_id=%s err=%v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load chats"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"chats": chats})
}

// ChatMessagesHandler handles GET /chat/:chat_id/messages.
func ChatMessagesHandler(c *gin.Context) {
	chatID := strings.TrimSpace(c.Param("chat_id"))
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	msgs, err := chatservice.Default().Messages(c.Request.Context(), user.ID, chatID, 50)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}

// ChatUploadsHandler handles GET /chat/:chat_id/uploads.
func ChatUploadsHandler(c *gin.Context) {
	chatID := strings.TrimSpace(c.Param("chat_id"))
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	uploads, err := chatservice.Default().Uploads(c.Request.Context(), user.ID, chatID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"uploads": uploads})
}

// DeleteChatHandler handles DELETE /chat/:chat_id.
func DeleteChatHandler(c *gin.Context) {
	chatID := strings.TrimSpace(c.Param("chat_id"))
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	if err := chatservice.Default().Delete(c.Request.Context(), user.ID, chatID); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Chat deleted successfully"})
}

// ClearContextHandler handles DELETE /context (deprecated multi-chat guard).
func ClearContextHandler(c *gin.Context) {
	log.Printf("[context] clear – disabled in multi-chat mode")
	c.JSON(http.StatusGone, gin.H{"error": "global context clearing is disabled in multi-chat mode"})
}
