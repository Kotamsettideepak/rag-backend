package groq

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (g *Client) doChatCompletionStream(ctx context.Context, payload chatCompletionRequest, stream chan string) error {
	if g.apiKey == "" {
		return fmt.Errorf("GROQ_API_KEY is required")
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= g.maxRetries; attempt++ {
		resp, err := g.doRequest(ctx, requestBody)
		if err == nil {
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
			lastErr = parseStream(resp.Body, stream)
			resp.Body.Close()
			if lastErr == nil {
				return nil
			}
		} else {
			lastErr = err
		}
		if attempt < g.maxRetries {
			time.Sleep(retryDelay("", attempt))
		}
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
		if chunk := parsed.Choices[0].Delta.Content; chunk != "" {
			stream <- chunk
		}
	}
	return scanner.Err()
}

func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}
