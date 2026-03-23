package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultJinaModel   = "jina-embeddings-v5-text-nano"
	defaultJinaTask    = "retrieval.query"
	defaultJinaTimeout = 60
)

func GetJinaAPIKey() string {
	return strings.TrimSpace(os.Getenv("JINA_API_KEY"))
}

func GetJinaBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("JINA_BASE_URL")), "/")
}

func GetJinaModel() string {
	model := strings.TrimSpace(os.Getenv("JINA_MODEL"))
	if model == "" {
		return defaultJinaModel
	}
	return model
}

func GetJinaTask() string {
	task := strings.TrimSpace(os.Getenv("JINA_TASK"))
	if task == "" {
		return defaultJinaTask
	}
	return task
}

func GetJinaTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("JINA_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultJinaTimeout * time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultJinaTimeout * time.Second
	}

	return time.Duration(seconds) * time.Second
}

func ValidateJinaConfig() error {
	if GetJinaBaseURL() == "" {
		return fmt.Errorf("JINA_BASE_URL is required")
	}
	return nil
}
