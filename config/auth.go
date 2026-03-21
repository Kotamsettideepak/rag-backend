package config

import (
	"os"
	"strings"
)

func GetGoogleClientID() string {
	return strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
}
