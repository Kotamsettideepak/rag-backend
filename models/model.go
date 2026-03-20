package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	OLLAMA_BASE_URL = "http://127.0.0.1:11434"
	MODEL_EMBEDDING = "nomic-embed-text:latest"
	EMBED_API       = "/api/embeddings"
)

type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type EmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

type OllamaClient struct {
	BaseURL string
	Client  *http.Client
}

func NewOllamaClient() *OllamaClient {
	timeout := 60 * time.Second
	if raw := os.Getenv("OLLAMA_TIMEOUT_SECONDS"); raw != "" {
		if seconds, err := time.ParseDuration(raw + "s"); err == nil {
			timeout = seconds
		}
	}

	return &OllamaClient{
		BaseURL: OLLAMA_BASE_URL,
		Client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (o *OllamaClient) postWithContext(ctx context.Context, endpoint string, payload interface{}) ([]byte, error) {
	url := o.BaseURL + endpoint

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama error: %s", string(body))
	}

	return body, nil
}

func (o *OllamaClient) GenerateEmbedding(text string) ([]float64, error) {
	return o.GenerateEmbeddingWithContext(context.Background(), text)
}

func (o *OllamaClient) GenerateEmbeddingWithContext(ctx context.Context, text string) ([]float64, error) {
	req := EmbeddingRequest{
		Model:  MODEL_EMBEDDING,
		Prompt: text,
	}

	body, err := o.postWithContext(ctx, EMBED_API, req)
	if err != nil {
		return nil, err
	}

	var res EmbeddingResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	return res.Embedding, nil
}
