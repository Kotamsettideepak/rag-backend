package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"gin-backend/config"
)

type Repository interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type JinaRepository struct {
	apiKeys    []string
	baseURL    string
	model      string
	task       string
	httpClient *http.Client
	nextKey    atomic.Uint64
}

type jinaRequest struct {
	Model      string   `json:"model"`
	Task       string   `json:"task"`
	Normalized bool     `json:"normalized"`
	Input      []string `json:"input"`
}

type jinaResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func NewJinaRepository(apiKeys []string) *JinaRepository {
	return &JinaRepository{
		apiKeys: append([]string(nil), apiKeys...),
		baseURL: config.GetJinaBaseURL(),
		model:   config.GetJinaModel(),
		task:    config.GetJinaTask(),
		httpClient: &http.Client{
			Timeout: config.GetJinaTimeout(),
		},
	}
}

func (r *JinaRepository) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	apiKey := r.pickAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("jina api key is not configured")
	}

	payload := jinaRequest{
		Model:      r.model,
		Task:       r.task,
		Normalized: true,
		Input:      texts,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal jina request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create jina request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send jina request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read jina response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jina embedding request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded jinaResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode jina response: %w", err)
	}

	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("jina returned %d embeddings for %d inputs", len(decoded.Data), len(texts))
	}

	embeddings := make([][]float32, 0, len(decoded.Data))
	for _, item := range decoded.Data {
		vector := make([]float32, len(item.Embedding))
		for index, value := range item.Embedding {
			vector[index] = float32(value)
		}
		embeddings = append(embeddings, vector)
	}

	return embeddings, nil
}

func (r *JinaRepository) pickAPIKey() string {
	if len(r.apiKeys) == 0 {
		return ""
	}
	index := r.nextKey.Add(1) - 1
	return r.apiKeys[index%uint64(len(r.apiKeys))]
}
