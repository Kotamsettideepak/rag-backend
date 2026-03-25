package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (g *Client) doChatCompletion(ctx context.Context, payload chatCompletionRequest) ([]byte, error) {
	if g.apiKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY is required")
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= g.maxRetries; attempt++ {
		resp, err := g.doRequest(ctx, requestBody)
		if err == nil {
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
		lastErr = err
		if attempt < g.maxRetries {
			time.Sleep(retryDelay("", attempt))
		}
	}
	return nil, fmt.Errorf("groq request failed after %d attempts: %w", g.maxRetries, lastErr)
}

func (g *Client) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return g.client.Do(req)
}
