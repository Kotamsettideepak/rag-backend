package api

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"gin-backend/ingest"
	"gin-backend/llm"
	"gin-backend/store"
	"gin-backend/voice"

	"github.com/gin-gonic/gin"
)

type VoiceChatResponse struct {
	Transcript    string `json:"transcript"`
	Answer        string `json:"answer"`
	AudioBase64   string `json:"audio_base64"`
	AudioMimeType string `json:"audio_mime_type"`
}

func VoiceChatHandler(c *gin.Context) {
	file, err := c.FormFile("audio")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is required"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read uploaded audio"})
		return
	}
	defer src.Close()

	payload, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read uploaded audio"})
		return
	}
	if len(payload) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is empty"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()

	voiceClient := voice.NewClient()
	transcription, err := voiceClient.Transcribe(ctx, file.Filename, payload, file.Header.Get("Content-Type"))
	if err != nil {
		log.Printf("[voice] speech-to-text failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to transcribe audio"})
		return
	}

	question := strings.TrimSpace(transcription.Text)
	if question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transcript was empty"})
		return
	}

	chatID := strings.TrimSpace(c.PostForm("chat_id"))
	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	if chatID != "" {
		if _, err := store.DefaultStore().GetChat(ctx, chatID, user.ID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
			return
		}
		if _, err := store.DefaultStore().SaveMessage(ctx, chatID, "user", question); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save user message"})
			return
		}
	}

	contextResult, err := ingest.DefaultManager().SearchContext(ctx, question, chatID, user.ID)
	if err != nil {
		log.Printf("[voice] failed to search vector context: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search for context"})
		return
	}

	conversationText := ""
	if chatID != "" {
		previousMessages, err := store.DefaultStore().ListMessages(ctx, chatID, recentContextMessages)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load messages"})
			return
		}
		logChatMessages("[voice]", chatID, previousMessages)
		uploads, err := store.DefaultStore().ListUserUploadedData(ctx, chatID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load uploaded data"})
			return
		}
		logUploadedData("[voice]", chatID, uploads)
		conversationText = buildConversationContext(previousMessages)
	}
	log.Printf("[voice] context modality=%s chars=%d preview=%s", contextResult.Modality, len(contextResult.Context), previewText(contextResult.Context, 320))

	prompt := buildPrompt(contextResult.Modality, contextResult.Context, conversationText, question)
	answer, err := llm.NewGroqClient().GenerateResponse([]llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Printf("[voice] groq response failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to generate answer"})
		return
	}

	if chatID != "" {
		if _, err := store.DefaultStore().SaveMessage(ctx, chatID, "assistant", answer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save assistant message"})
			return
		}
	}

	audioResponse, err := voiceClient.Synthesize(ctx, answer)
	if err != nil {
		log.Printf("[voice] text-to-speech failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to synthesize audio"})
		return
	}

	c.JSON(http.StatusOK, VoiceChatResponse{
		Transcript:    question,
		Answer:        answer,
		AudioBase64:   audioResponse.AudioBase64,
		AudioMimeType: audioResponse.MimeType,
	})
}
