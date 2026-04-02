package groq

import (
	"bufio"
	"bytes"
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
	StreamResponse(messages []Message, stream chan StreamEvent) error
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
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Response string `json:"response"`
}

type streamChunkResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Thinking string `json:"thinking"`
	Content string `json:"content"`
}

type StreamEvent struct {
	Thinking string
	Content  string
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
	return extractResponseText(body)
}

func (g *Client) StreamResponse(messages []Message, stream chan StreamEvent) error {
	return g.doChatCompletionStream(context.Background(), chatCompletionRequest{
		Messages: messages,
		Stream:   true,
	}, stream)
}

func (g *Client) usesColabAPI() bool {
	base := strings.ToLower(strings.TrimSpace(g.baseURL))
	return strings.Contains(base, ".loca.lt") || strings.HasSuffix(base, "/v1/chat")
}

func extractResponseText(body []byte) (string, error) {
	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err == nil {
		if len(parsed.Choices) > 0 && strings.TrimSpace(parsed.Choices[0].Message.Content) != "" {
			return parsed.Choices[0].Message.Content, nil
		}
		if strings.TrimSpace(parsed.Message.Content) != "" {
			return parsed.Message.Content, nil
		}
		if strings.TrimSpace(parsed.Response) != "" {
			return parsed.Response, nil
		}
	}

	// Some local tunnel test deployments return an SSE body even for non-stream
	// callers, so recover the text from accumulated data: ... lines.
	if text := extractSSEText(body); strings.TrimSpace(text) != "" {
		return text, nil
	}

	return "", fmt.Errorf("llm response contained no content")
}

func extractSSEText(body []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var parsed streamChunkResponse
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			continue
		}
		if len(parsed.Choices) > 0 && parsed.Choices[0].Delta.Content != "" {
			out.WriteString(parsed.Choices[0].Delta.Content)
			continue
		}
		if parsed.Content != "" {
			out.WriteString(parsed.Content)
		}
	}

	return out.String()
}
