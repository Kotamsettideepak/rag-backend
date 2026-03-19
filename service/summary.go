package service

import (
	"fmt"
	"log"
	"strings"

	"gin-backend/models"
)

func GenerateUploadSummary(extractedTexts []string) string {
	joined := strings.TrimSpace(strings.Join(extractedTexts, "\n\n"))
	if joined == "" {
		return "Upload completed. I am ready to answer questions about the uploaded content."
	}

	preview := buildPreviewText(joined, 1800)
	prompt := fmt.Sprintf(
		"Here is extracted content from the user's latest upload:\n\n%s\n\nGive a very short summary in 2 to 4 lines. Mention what the uploaded content appears to be and the most important details only. Do not say you are an AI. Do not add anything not present in the content.",
		preview,
	)

	client := models.NewOllamaClient()
	summary, err := client.GenerateText(prompt)
	if err != nil {
		log.Printf("[summary] failed to generate upload summary with llm: %v", err)
		return preview
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return preview
	}

	log.Printf("[summary] upload summary generated: chars=%d", len(summary))
	return summary
}

func buildPreviewText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
