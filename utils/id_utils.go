package utils

import "github.com/google/uuid"

func GenerateChatID() string {
	return uuid.New().String()
}