package config

import (
	"os"
	"strings"
)

func GetTopicIngestInternalToken() string {
	return strings.TrimSpace(os.Getenv("TOPIC_INGEST_INTERNAL_TOKEN"))
}
