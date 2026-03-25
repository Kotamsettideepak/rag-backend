package groq

import (
	"context"
	"encoding/json"
	"fmt"
	"gin-backend/config"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGroqModel = "llama-3.1-8b-instant"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMClient interface {
	GenerateResponse(messages []Message) (string, error)
	StreamResponse(messages []Message, stream chan string) error
}

type Client struct {
	baseURL    string
	apiKey     string
	models     []string
	maxRetries int
	client     *http.Client
}

type chatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type streamChunkResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func NewClient() LLMClient {
	timeout := 90 * time.Second
	if raw := os.Getenv("GROQ_TIMEOUT_SECONDS"); raw != "" {
		if seconds, err := time.ParseDuration(raw + "s"); err == nil {
			timeout = seconds
		}
	}

	maxRetries := 3
	if raw := os.Getenv("GROQ_MAX_RETRIES"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			maxRetries = value
		}
	}

	baseURL := config.GetGroqBaseURL()

	model := strings.TrimSpace(os.Getenv("GROQ_MODEL"))
	if model == "" {
		model = defaultGroqModel
	}

	return &Client{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		models:     uniqueModels(model, config.GetGroqFallbackModels()),
		maxRetries: maxRetries,
		client:     &http.Client{Timeout: timeout},
	}
}

func CurrentModel() string {
	model := strings.TrimSpace(os.Getenv("GROQ_MODEL"))
	if model == "" {
		return defaultGroqModel
	}
	return model
}

func (g *Client) GenerateResponse(messages []Message) (string, error) {
	body, err := g.doChatCompletion(context.Background(), chatCompletionRequest{
		Messages: messages,
	})
	if err != nil {
		return "", err
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("groq response contained no choices")
	}

	return parsed.Choices[0].Message.Content, nil
}

func (g *Client) StreamResponse(messages []Message, stream chan string) error {
	return g.doChatCompletionStream(context.Background(), chatCompletionRequest{
		Messages: messages,
		Stream:   true,
	}, stream)
}
