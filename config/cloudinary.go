package config

import (
	"os"
	"strings"
)

const defaultCloudinaryBaseURL = "https://api.cloudinary.com/v1_1"

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
	baseURL := strings.TrimSpace(os.Getenv("CLOUDINARY_BASE_URL"))
	if baseURL == "" {
		return defaultCloudinaryBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}
