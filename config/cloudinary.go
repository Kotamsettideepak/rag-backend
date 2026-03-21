package config

import "strings"
import "os"

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
