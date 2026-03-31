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

func GetTopicExtractorBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("TOPIC_EXTRACTOR_BASE_URL")), "/")
}

func ValidateExtractorConfig() error {
	return validateAbsoluteURL("EXTRACTOR_BASE_URL", GetExtractorBaseURL())
}

func ValidateTopicExtractorConfig() error {
	return validateAbsoluteURL("TOPIC_EXTRACTOR_BASE_URL", GetTopicExtractorBaseURL())
}

func validateAbsoluteURL(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid absolute URL", name)
	}
	return nil
}
