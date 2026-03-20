package config

import (
	"os"
	"strings"
)

const defaultExtractorBaseURL = "http://127.0.0.1:8090"

func GetExtractorBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("EXTRACTOR_BASE_URL"))
	if baseURL == "" {
		return defaultExtractorBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}
