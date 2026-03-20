package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultGroqAudioModel = "whisper-large-v3-turbo"

func GetGroqAPIKey() string {
	return strings.TrimSpace(os.Getenv("GROQ_API_KEY"))
}

func GetGroqBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("GROQ_BASE_URL"))
	if baseURL == "" {
		return "https://api.groq.com/openai/v1"
	}
	return strings.TrimRight(baseURL, "/")
}

func GetGroqAudioModel() string {
	model := strings.TrimSpace(os.Getenv("GROQ_AUDIO_MODEL"))
	if model == "" {
		return defaultGroqAudioModel
	}
	return model
}

func GetGroqTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GROQ_TIMEOUT_SECONDS"))
	if raw == "" {
		return 180 * time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 180 * time.Second
	}

	return time.Duration(seconds) * time.Second
}
