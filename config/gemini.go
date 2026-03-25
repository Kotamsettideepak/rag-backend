package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGeminiModel = "gemini-2.5-flash"
)

func GetGeminiAPIKey() string {
	return strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
}

func GetGeminiBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("GEMINI_BASE_URL")), "/")
}

func GetGeminiModel() string {
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		return defaultGeminiModel
	}
	return model
}

func GetGeminiTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GEMINI_TIMEOUT_SECONDS"))
	if raw == "" {
		return 90 * time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 90 * time.Second
	}

	return time.Duration(seconds) * time.Second
}

func ValidateGeminiConfig() error {
	if GetGeminiBaseURL() == "" {
		return fmt.Errorf("GEMINI_BASE_URL is required")
	}
	return nil
}
