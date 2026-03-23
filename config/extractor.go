package config

import (
	"net/url"
	"os"
	"strings"
)

const defaultExtractorBaseURL = "http://127.0.0.1:8090"
const defaultExtractorBindHost = "127.0.0.1"
const defaultExtractorPort = "8090"

func GetExtractorBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("EXTRACTOR_BASE_URL"))
	if baseURL == "" {
		return defaultExtractorBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func GetExtractorBindHost() string {
	host := strings.TrimSpace(os.Getenv("EXTRACTOR_BIND_HOST"))
	if host != "" {
		return host
	}

	parsed, err := url.Parse(GetExtractorBaseURL())
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}

	return defaultExtractorBindHost
}

func GetExtractorPort() string {
	port := strings.TrimSpace(os.Getenv("EXTRACTOR_PORT"))
	if port != "" {
		return port
	}

	parsed, err := url.Parse(GetExtractorBaseURL())
	if err == nil && parsed.Port() != "" {
		return parsed.Port()
	}

	return defaultExtractorPort
}
