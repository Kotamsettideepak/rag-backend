package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDeepgramTTSModel    = "aura-2-thalia-en"
	defaultDeepgramSTTModel    = "nova-3"
	defaultDeepgramTTSEncoding = "mp3"
)

func GetDeepgramAPIKey() string {
	return strings.TrimSpace(os.Getenv("DEEPGRAM_API_KEY"))
}

func GetDeepgramBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("DEEPGRAM_BASE_URL")), "/")
}

func GetDeepgramTTSModel() string {
	model := strings.TrimSpace(os.Getenv("DEEPGRAM_TTS_MODEL"))
	if model == "" {
		return defaultDeepgramTTSModel
	}
	return model
}

func GetDeepgramSTTModel() string {
	model := strings.TrimSpace(os.Getenv("DEEPGRAM_STT_MODEL"))
	if model == "" {
		return defaultDeepgramSTTModel
	}
	return model
}

func GetDeepgramTTSEncoding() string {
	encoding := strings.TrimSpace(os.Getenv("DEEPGRAM_TTS_ENCODING"))
	if encoding == "" {
		return defaultDeepgramTTSEncoding
	}
	return encoding
}

func GetDeepgramTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("DEEPGRAM_TIMEOUT_SECONDS"))
	if raw == "" {
		return 120 * time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 120 * time.Second
	}

	return time.Duration(seconds) * time.Second
}

func ValidateDeepgramConfig() error {
	if GetDeepgramBaseURL() == "" {
		return fmt.Errorf("DEEPGRAM_BASE_URL is required")
	}
	return nil
}
