package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gin-backend/config"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGroqModel   = "llama-3.1-8b-instant"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMClient interface {
	GenerateResponse(messages []Message) (string, error)
	StreamResponse(messages []Message, stream chan string) error
}

type GroqClient struct {
	baseURL    string
	apiKey     string
	model      string
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

func NewGroqClient() LLMClient {
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

	return &GroqClient{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		model:      model,
		maxRetries: maxRetries,
		client:     &http.Client{Timeout: timeout},
	}
}

func CurrentGroqModel() string {
	model := strings.TrimSpace(os.Getenv("GROQ_MODEL"))
	if model == "" {
		return defaultGroqModel
	}
	return model
}

func (g *GroqClient) GenerateResponse(messages []Message) (string, error) {
	body, err := g.doChatCompletion(context.Background(), chatCompletionRequest{
		Model:    g.model,
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

func (g *GroqClient) StreamResponse(messages []Message, stream chan string) error {
	return g.doChatCompletionStream(context.Background(), chatCompletionRequest{
		Model:    g.model,
		Messages: messages,
		Stream:   true,
	}, stream)
}

func (g *GroqClient) doChatCompletion(ctx context.Context, payload chatCompletionRequest) ([]byte, error) {
	if g.apiKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY is required")
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= g.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewBuffer(requestBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := g.client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			if resp.StatusCode == http.StatusOK {
				return body, nil
			}

			lastErr = fmt.Errorf("groq chat completion failed with status %d: %s", resp.StatusCode, string(body))
			if !shouldRetry(resp.StatusCode) || attempt == g.maxRetries {
				return nil, lastErr
			}
			time.Sleep(retryDelay(resp.Header.Get("Retry-After"), attempt))
			continue
		}

		if attempt == g.maxRetries {
			break
		}
		time.Sleep(retryDelay("", attempt))
	}

	return nil, fmt.Errorf("groq request failed after %d attempts: %w", g.maxRetries, lastErr)
}

func (g *GroqClient) doChatCompletionStream(ctx context.Context, payload chatCompletionRequest, stream chan string) error {
	if g.apiKey == "" {
		return fmt.Errorf("GROQ_API_KEY is required")
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= g.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewBuffer(requestBody))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := g.client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				lastErr = fmt.Errorf("groq streaming failed with status %d: %s", resp.StatusCode, string(body))
				if !shouldRetry(resp.StatusCode) || attempt == g.maxRetries {
					return lastErr
				}
				time.Sleep(retryDelay(resp.Header.Get("Retry-After"), attempt))
				continue
			}

			err = parseStream(resp.Body, stream)
			resp.Body.Close()
			if err == nil {
				return nil
			}
			lastErr = err
		}

		if attempt == g.maxRetries {
			break
		}
		time.Sleep(retryDelay("", attempt))
	}

	return fmt.Errorf("groq stream failed after %d attempts: %w", g.maxRetries, lastErr)
}

func parseStream(body io.Reader, stream chan string) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return nil
		}

		var parsed streamChunkResponse
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			return err
		}
		if len(parsed.Choices) == 0 {
			continue
		}

		chunk := parsed.Choices[0].Delta.Content
		if chunk != "" {
			stream <- chunk
		}
	}

	return scanner.Err()
}

func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	return time.Duration(attempt) * 500 * time.Millisecond
}
