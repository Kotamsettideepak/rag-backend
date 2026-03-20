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

	contextResult, err := ingest.DefaultManager().SearchContext(ctx, question)
	if err != nil {
		log.Printf("[voice] failed to search vector context: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search for context"})
		return
	}

	prompt := buildPrompt(contextResult.Modality, contextResult.Context, question)
	answer, err := llm.NewGroqClient().GenerateResponse([]llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Printf("[voice] groq response failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to generate answer"})
		return
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
