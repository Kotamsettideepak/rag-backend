package voice

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"gin-backend/middleware"
	chatservice "gin-backend/service/chat"

	"github.com/gin-gonic/gin"
)

// VoiceChatHandler handles POST /voice/chat.
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
	if err != nil || len(payload) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is empty or unreadable"})
		return
	}

	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}

	result, err := chatservice.Default().VoiceAnswer(
		c.Request.Context(),
		user.ID,
		"",
		file.Filename,
		file.Header.Get("Content-Type"),
		payload,
	)
	if err != nil {
		log.Printf("[voice] request failed: %v", err)
		switch {
		case errors.Is(err, chatservice.ErrEmptyTranscript):
			c.JSON(http.StatusBadRequest, gin.H{"error": "Try asking a question."})
		case strings.Contains(strings.ToLower(err.Error()), "not found"):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"transcript": result.Transcript,
	})
}
