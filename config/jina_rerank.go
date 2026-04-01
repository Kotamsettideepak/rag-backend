package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultJinaRerankBaseURL = "https://api.jina.ai/v1/rerank"
	defaultJinaRerankModel   = "jina-reranker-v2-base-multilingual"
)

func GetJinaRerankBaseURL() string {
	value := strings.TrimRight(strings.TrimSpace(os.Getenv("JINA_RERANK_BASE_URL")), "/")
	if value == "" {
		return defaultJinaRerankBaseURL
	}
	return value
}

func GetJinaRerankModel() string {
	value := strings.TrimSpace(os.Getenv("JINA_RERANK_MODEL"))
	if value == "" {
		return defaultJinaRerankModel
	}
	return value
}

func GetJinaRerankTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("JINA_RERANK_TIMEOUT_SECONDS"))
	if raw == "" {
		return GetJinaTimeout()
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return GetJinaTimeout()
	}

	return time.Duration(seconds) * time.Second
}

func IsJinaRerankEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("JINA_RERANK_ENABLED")))
	if raw == "" {
		return true
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}
