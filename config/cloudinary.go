package config

import (
	"fmt"
	"os"
	"strings"
)

func GetCloudinaryCloudName() string {
	return strings.TrimSpace(os.Getenv("CLOUDINARY_CLOUD_NAME"))
}

func GetCloudinaryAPIKey() string {
	return strings.TrimSpace(os.Getenv("CLOUDINARY_API_KEY"))
}

func GetCloudinaryAPISecret() string {
	return strings.TrimSpace(os.Getenv("CLOUDINARY_API_SECRET"))
}

func GetCloudinaryFolder() string {
	folder := strings.TrimSpace(os.Getenv("CLOUDINARY_FOLDER"))
	if folder == "" {
		return "rag-ai"
	}
	return folder
}

func GetCloudinaryBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("CLOUDINARY_BASE_URL")), "/")
}

func ValidateCloudinaryConfig() error {
	if GetCloudinaryBaseURL() == "" {
		return fmt.Errorf("CLOUDINARY_BASE_URL is required")
	}
	return nil
}
