package config

import (
	"fmt"
	"os"
	"strings"
)

func GetQuizGeneratorBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("QUIZ_GENERATOR_BASE_URL")), "/")
}

func GetEvaluationServiceBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("EVALUATION_SERVICE_BASE_URL")), "/")
}

func GetQuizInternalToken() string {
	return strings.TrimSpace(os.Getenv("QUIZ_INTERNAL_TOKEN"))
}

func ValidateQuizServicesConfig() error {
	if GetQuizGeneratorBaseURL() == "" {
		return fmt.Errorf("QUIZ_GENERATOR_BASE_URL is required")
	}
	if GetEvaluationServiceBaseURL() == "" {
		return fmt.Errorf("EVALUATION_SERVICE_BASE_URL is required")
	}
	return nil
}
