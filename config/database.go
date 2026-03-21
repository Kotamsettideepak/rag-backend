package config

import (
	"os"
	"strings"
)

func GetDatabaseURL() string {
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}
