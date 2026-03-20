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

	contextText, err := ingest.DefaultManager().SearchContext(ctx, req.Question)
	if err != nil {
		log.Printf("[chat] failed to search vector context: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search for context: " + err.Error()})
		return
	}

	prompt := fmt.Sprintf(
		"You are answering questions only from the uploaded content.\n\nRetrieved context:\n%s\n\nUser question:\n%s\n\nInstructions:\n- Answer using only the retrieved context.\n- If the answer is not clearly present, say that it is not available in the uploaded content.\n- Be concise but complete.\n- When possible, quote exact values from the context.\n- If the question asks about file name, file type, extension, or document identity, prefer explicit uploaded file metadata over text printed inside the document body.\n- Do not confuse the uploaded filename with titles, internal chapter names, print marks, or layout/source file names appearing inside the document.",
		contextText,
		req.Question,
	)

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
