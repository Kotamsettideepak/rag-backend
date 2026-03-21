package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	OLLAMA_BASE_URL = "http://127.0.0.1:11434"
	MODEL_EMBEDDING = "nomic-embed-text:latest"
	EMBED_API       = "/api/embed"
)

type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type BatchEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type EmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

type BatchEmbeddingResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
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
	log.Printf("[embedding] query embedding model=%s input_chars=%d preview=%s", MODEL_EMBEDDING, len(text), previewText(text, 180))
	embeddings, err := o.GenerateEmbeddingsWithContext(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) != 1 {
		return nil, fmt.Errorf("ollama returned %d embeddings for a single input", len(embeddings))
	}

	return embeddings[0], nil
}

func (o *OllamaClient) GenerateEmbeddingsWithContext(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	log.Printf(
		"[embedding] batch embedding model=%s batch_size=%d first_input_chars=%d first_preview=%s",
		MODEL_EMBEDDING,
		len(texts),
		len(texts[0]),
		previewText(texts[0], 180),
	)

	req := BatchEmbeddingRequest{
		Model: MODEL_EMBEDDING,
		Input: texts,
	}

	body, err := o.postWithContext(ctx, EMBED_API, req)
	if err != nil {
		return nil, err
	}

	var res BatchEmbeddingResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	if len(res.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(res.Embeddings), len(texts))
	}
	if len(res.Embeddings) > 0 {
		log.Printf("[embedding] batch embedding completed vectors=%d dims=%d", len(res.Embeddings), len(res.Embeddings[0]))
	}

	return res.Embeddings, nil
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
