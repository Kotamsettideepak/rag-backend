package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gin-backend/llm"
)

func GenerateUploadSummary(extractedTexts []string) string {
	started := time.Now()
	joined := strings.TrimSpace(strings.Join(extractedTexts, "\n\n"))
	if joined == "" {
		return "Upload completed. I am ready to answer questions about the uploaded content."
	}

	preview := buildPreviewText(joined, 1800)
	prompt := fmt.Sprintf(
		"Here is extracted content from the user's latest upload:\n\n%s\n\nGive a very short summary in 2 to 4 lines. Mention what the uploaded content appears to be and the most important details only. Do not say you are an AI. Do not add anything not present in the content.",
		preview,
	)

	client := llm.NewGroqClient()
	summary, err := client.GenerateResponse([]llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		log.Printf("[summary] failed to generate upload summary with llm: %v", err)
		return preview
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return preview
	}

	log.Printf("======== [UPLOAD SUMMARY] ============")
	log.Printf("[summary] chars=%d duration=%s preview=%q", len(summary), time.Since(started), buildPreviewText(summary, 220))
	log.Printf("======================================")
	return summary
}

func buildPreviewText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
