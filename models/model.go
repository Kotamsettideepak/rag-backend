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

///////////////////////////////////////////////////////////
// 🔹 Constants (Model Names + Config)
///////////////////////////////////////////////////////////

const (
	OLLAMA_BASE_URL = "http://127.0.0.1:11434"

	// Models
	MODEL_LLM       = "llama3:latest"
	MODEL_EMBEDDING = "nomic-embed-text:latest"
	MODEL_VISION    = "llava:latest"
	MODEL_AUDIO     = "whisper:latest"

	// Endpoints
	GENERATE_API = "/api/generate"
	EMBED_API    = "/api/embeddings"
)

///////////////////////////////////////////////////////////
// 🔹 Request / Response Structs
///////////////////////////////////////////////////////////

type GenerateRequest struct {
	Model  string   `json:"model"`
	Prompt string   `json:"prompt"`
	Images []string `json:"images,omitempty"`
	Stream bool     `json:"stream"`
}

type GenerateResponse struct {
	Response string `json:"response"`
}

type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type EmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

///////////////////////////////////////////////////////////
// 🔹 Ollama Client
///////////////////////////////////////////////////////////

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

///////////////////////////////////////////////////////////
// 🔹 Core Request Handler
///////////////////////////////////////////////////////////

func (o *OllamaClient) post(endpoint string, payload interface{}) ([]byte, error) {
	return o.postWithContext(context.Background(), endpoint, payload)
}

func (o *OllamaClient) postWithContext(ctx context.Context, endpoint string, payload interface{}) ([]byte, error) {
	url := o.BaseURL + endpoint

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
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

///////////////////////////////////////////////////////////
// 🔹 LLM: Text Generation
///////////////////////////////////////////////////////////

func (o *OllamaClient) GenerateText(prompt string) (string, error) {
	req := GenerateRequest{
		Model:  MODEL_LLM,
		Prompt: prompt,
		Stream: false,
	}

	body, err := o.post(GENERATE_API, req)
	if err != nil {
		return "", err
	}

	var res GenerateResponse
	err = json.Unmarshal(body, &res)
	if err != nil {
		return "", err
	}

	return res.Response, nil
}

///////////////////////////////////////////////////////////
// 🔹 Embeddings
///////////////////////////////////////////////////////////

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
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	return res.Embedding, nil
}

///////////////////////////////////////////////////////////
// 🔹 Vision (Image → Text)
///////////////////////////////////////////////////////////

func (o *OllamaClient) DescribeImage(prompt string, base64Image string) (string, error) {
	req := GenerateRequest{
		Model:  MODEL_VISION,
		Prompt: prompt,
		Images: []string{base64Image},
		Stream: false,
	}

	body, err := o.post(GENERATE_API, req)
	if err != nil {
		return "", err
	}

	var res GenerateResponse
	err = json.Unmarshal(body, &res)
	if err != nil {
		return "", err
	}

	return res.Response, nil
}

///////////////////////////////////////////////////////////
// 🔹 Audio (Placeholder for Whisper)
///////////////////////////////////////////////////////////

// NOTE: Ollama Whisper support depends on setup.
// You may need external API or CLI integration.
func (o *OllamaClient) TranscribeAudio(filePath string) (string, error) {
	return "", fmt.Errorf("whisper integration not implemented yet")
}
