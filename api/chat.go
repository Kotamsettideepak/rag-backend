package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"gin-backend/ingest"
	"gin-backend/llm"
	"gin-backend/trace"

	"github.com/gin-gonic/gin"
)

type ChatRequest struct {
	Question string `json:"question"`
}

func ChatHandler(c *gin.Context) {
	trace.Start("CHAT", c.Request.URL.Path)
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
	log.Printf("[chat] question=%s", previewText(req.Question, 220))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	contextResult, err := ingest.DefaultManager().SearchContext(ctx, req.Question)
	if err != nil {
		log.Printf("[chat] failed to search vector context: %v", err)
		trace.End("CHAT", "failed during retrieval")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search for context: " + err.Error()})
		return
	}

	log.Printf("[chat] context modality=%s", contextResult.Modality)
	log.Printf("[chat] context preview=%s", previewText(contextResult.Context, 320))
	prompt := buildPrompt(contextResult.Modality, contextResult.Context, req.Question)
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

	c.JSON(http.StatusOK, gin.H{
		"answer": answer,
	})
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
