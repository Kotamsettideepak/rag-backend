package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

func GetExtractorBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("EXTRACTOR_BASE_URL")), "/")
}

func ValidateExtractorConfig() error {
	baseURL := GetExtractorBaseURL()
	if baseURL == "" {
		return fmt.Errorf("EXTRACTOR_BASE_URL is required")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("EXTRACTOR_BASE_URL must be a valid absolute URL")
	}

	return nil
}
