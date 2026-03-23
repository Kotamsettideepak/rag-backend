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

func GetGroqAudioRequestInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GROQ_AUDIO_REQUEST_INTERVAL_MS"))
	if raw == "" {
		return 3500 * time.Millisecond
	}

	milliseconds, err := strconv.Atoi(raw)
	if err != nil || milliseconds < 0 {
		return 3500 * time.Millisecond
	}

	return time.Duration(milliseconds) * time.Millisecond
}

func GetGroqAudioMaxRetries() int {
	raw := strings.TrimSpace(os.Getenv("GROQ_AUDIO_MAX_RETRIES"))
	if raw == "" {
		return 4
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 4
	}

	return value
}

func GetGroqAudioChunkSizeSeconds() float64 {
	raw := strings.TrimSpace(os.Getenv("GROQ_AUDIO_CHUNK_SIZE_SECONDS"))
	if raw == "" {
		return 60
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 60
	}

	return value
}

func GetGroqAudioChunkOverlapSeconds() float64 {
	raw := strings.TrimSpace(os.Getenv("GROQ_AUDIO_CHUNK_OVERLAP_SECONDS"))
	if raw == "" {
		return 10
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		return 10
	}

	chunkSize := GetGroqAudioChunkSizeSeconds()
	if value >= chunkSize {
		return chunkSize / 2
	}

	return value
}
