package config

import (
	"os"
	"strings"
)

const defaultAudioServiceBaseURL = "http://127.0.0.1:8001"

func GetAudioServiceBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("AUDIO_SERVICE_URL"))
	if baseURL == "" {
		return defaultAudioServiceBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}
