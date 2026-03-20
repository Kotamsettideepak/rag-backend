package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultElevenLabsBaseURL      = "https://api.elevenlabs.io/v1"
	defaultElevenLabsVoiceID      = "JBFqnCBsd6RMkjVDRZzb"
	defaultElevenLabsTTSModel     = "eleven_multilingual_v2"
	defaultElevenLabsSTTModel     = "scribe_v2"
	defaultElevenLabsOutputFormat = "mp3_44100_128"
)

func GetElevenLabsAPIKey() string {
	return strings.TrimSpace(os.Getenv("ELEVENLABS_API_KEY"))
}

func GetElevenLabsBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("ELEVENLABS_BASE_URL"))
	if baseURL == "" {
		return defaultElevenLabsBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func GetElevenLabsVoiceID() string {
	voiceID := strings.TrimSpace(os.Getenv("ELEVENLABS_VOICE_ID"))
	if voiceID == "" {
		return defaultElevenLabsVoiceID
	}
	return voiceID
}

func GetElevenLabsTTSModel() string {
	model := strings.TrimSpace(os.Getenv("ELEVENLABS_TTS_MODEL"))
	if model == "" {
		return defaultElevenLabsTTSModel
	}
	return model
}

func GetElevenLabsSTTModel() string {
	model := strings.TrimSpace(os.Getenv("ELEVENLABS_STT_MODEL"))
	if model == "" {
		return defaultElevenLabsSTTModel
	}
	return model
}

func GetElevenLabsOutputFormat() string {
	format := strings.TrimSpace(os.Getenv("ELEVENLABS_OUTPUT_FORMAT"))
	if format == "" {
		return defaultElevenLabsOutputFormat
	}
	return format
}

func GetElevenLabsTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("ELEVENLABS_TIMEOUT_SECONDS"))
	if raw == "" {
		return 120 * time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 120 * time.Second
	}

	return time.Duration(seconds) * time.Second
}
