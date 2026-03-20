package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"gin-backend/ingest"
	"gin-backend/llm"

	"github.com/gin-gonic/gin"
)

type ChatRequest struct {
	Question string `json:"question"`
}

func ChatHandler(c *gin.Context) {
	log.Printf("[chat] request received: method=%s path=%s", c.Request.Method, c.Request.URL.Path)

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	contextResult, err := ingest.DefaultManager().SearchContext(ctx, req.Question)
	if err != nil {
		log.Printf("[chat] failed to search vector context: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search for context: " + err.Error()})
		return
	}

	log.Printf("[chat] context modality=%s", contextResult.Modality)
	prompt := buildPrompt(contextResult.Modality, contextResult.Context, req.Question)

	client := llm.NewGroqClient()
	answer, err := client.GenerateResponse([]llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Printf("[chat] failed to generate answer: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate text response"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"answer": answer,
	})
}
