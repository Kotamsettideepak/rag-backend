package ingestion

import (
	"os"
	"strconv"
	"strings"
)

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func stageLabel(stage string) string {
	labels := map[string]string{
		"queued":       "Queued. Waiting to start processing.",
		"processing":   "Preparing your files for AI processing.",
		"chat_ready":   "Initial content is indexed. You can start chatting while the rest is still being processed.",
		"extracting":   "Extracting data from your files.",
		"converting":   "Converting uploaded video into audio.",
		"transcribing": "Transcribing audio into searchable text.",
		"chunking":     "Normalizing and organizing the extracted content.",
		"embedding":    "Creating embeddings so your content can be searched semantically.",
		"storing":      "Saving everything to the vector database.",
		"completed":    "Your files are ready. You can start chatting now.",
		"failed":       "Processing failed.",
	}
	if label, ok := labels[strings.ToLower(strings.TrimSpace(stage))]; ok {
		return label
	}
	return "Processing your files."
}
