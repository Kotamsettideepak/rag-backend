package config

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultChromaCloudHost = "api.trychroma.com"
	defaultChromaTenant    = "default_tenant"
	defaultChromaDatabase  = "default_database"
)

func getChromaHost() string {
	host := strings.TrimSpace(os.Getenv("CHROMA_HOST"))
	if host == "" {
		return defaultChromaCloudHost
	}
	return host
}

func GetChromaBaseURL() string {
	host := getChromaHost()
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "https://" + host
}

func GetChromaTenant() string {
	tenant := strings.TrimSpace(os.Getenv("CHROMA_TENANT"))
	if tenant == "" {
		return defaultChromaTenant
	}
	return tenant
}

func GetChromaDatabase() string {
	database := strings.TrimSpace(os.Getenv("CHROMA_DATABASE"))
	if database == "" {
		return defaultChromaDatabase
	}
	return database
}

func GetChromaAPIKey() string {
	return strings.TrimSpace(os.Getenv("CHROMA_API_KEY"))
}

func EnsureChromaRunning() error {
	log.Printf("[startup] using Chroma Cloud only: host=%s tenant=%s database=%s", GetChromaBaseURL(), GetChromaTenant(), GetChromaDatabase())

	if GetChromaAPIKey() == "" {
		return fmt.Errorf("CHROMA_API_KEY is required")
	}

	return ensureChromaHTTPReachable()
}

func ensureChromaHTTPReachable() error {
	url := GetChromaBaseURL() + "/api/v2/auth/identity"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("x-chroma-token", GetChromaAPIKey())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach chroma endpoint %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chroma health/auth check failed with status %d", resp.StatusCode)
	}

	log.Printf("[startup] chroma cloud endpoint reachable: url=%s", url)
	return nil
}
