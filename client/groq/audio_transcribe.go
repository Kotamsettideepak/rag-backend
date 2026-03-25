package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (c *AudioHTTPClient) transcribeChunkFile(ctx context.Context, chunkPath, _ string, chunkIndex int) (transcriptionResponse, error) {
	fileData, err := os.ReadFile(chunkPath)
	if err != nil {
		return transcriptionResponse{}, err
	}
	if len(fileData) == 0 {
		return transcriptionResponse{}, fmt.Errorf("audio chunk %d is empty", chunkIndex)
	}
	if len(fileData) > maxTranscriptionBytes {
		return transcriptionResponse{}, fmt.Errorf("audio chunk %d exceeds current transcription limit of 25 MB", chunkIndex)
	}
	return c.transcribeFileData(ctx, fileData, filepath.Base(chunkPath), chunkIndex)
}

func (c *AudioHTTPClient) transcribeFileData(ctx context.Context, fileData []byte, fileName string, chunkIndex int) (transcriptionResponse, error) {
	body, contentType, err := buildAudioMultipartBody(fileData, fileName, c.audioModel)
	if err != nil {
		return transcriptionResponse{}, err
	}
	endpoint := c.baseURL + "/audio/transcriptions"
	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		if chunkIndex >= 0 {
			if err := c.waitForTranscriptionSlot(ctx); err != nil {
				return transcriptionResponse{}, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return transcriptionResponse{}, err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		logTranscriptionRequest(endpoint, fileName, chunkIndex, attempt, c.audioModel, len(fileData))

		resp, err := c.client.Do(req)
		if err != nil {
			if attempt == c.maxRetries {
				return transcriptionResponse{}, err
			}
			if err := waitForRetry(ctx, time.Duration(attempt)*2*time.Second); err != nil {
				return transcriptionResponse{}, err
			}
			continue
		}

		responseBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return transcriptionResponse{}, readErr
		}
		logTranscriptionResponse(resp.StatusCode, fileName, chunkIndex, attempt, responseBody)
		if resp.StatusCode == http.StatusOK {
			var parsed transcriptionResponse
			if err := json.Unmarshal(responseBody, &parsed); err != nil {
				return transcriptionResponse{}, err
			}
			return parsed, nil
		}
		if !shouldRetryTranscription(resp.StatusCode) || attempt == c.maxRetries {
			return transcriptionResponse{}, fmt.Errorf("groq transcription failed with status %d: %s", resp.StatusCode, string(responseBody))
		}
		if err := waitForRetry(ctx, transcriptionRetryDelay(resp.Header.Get("Retry-After"), responseBody, attempt)); err != nil {
			return transcriptionResponse{}, err
		}
	}
	return transcriptionResponse{}, fmt.Errorf("groq transcription failed after %d attempts", c.maxRetries)
}

func buildAudioMultipartBody(fileData []byte, fileName, modelName string) ([]byte, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(fileName))
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(fileData); err != nil {
		return nil, "", err
	}
	for key, value := range map[string]string{
		"model":                     modelName,
		"response_format":           "verbose_json",
		"timestamp_granularities[]": "segment",
	} {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func logTranscriptionRequest(endpoint, fileName string, chunkIndex, attempt int, modelName string, size int) {
	if chunkIndex >= 0 {
		log.Printf("[audio] sending Groq transcription request url=%s file=%s chunk=%d attempt=%d model=%s bytes=%d",
			endpoint, filepath.Base(fileName), chunkIndex, attempt, modelName, size)
		return
	}
	log.Printf("[audio] sending Groq transcription request url=%s file=%s mode=direct attempt=%d model=%s bytes=%d",
		endpoint, filepath.Base(fileName), attempt, modelName, size)
}

func logTranscriptionResponse(status int, fileName string, chunkIndex, attempt int, body []byte) {
	if chunkIndex >= 0 {
		log.Printf("[audio] response status=%d file=%s chunk=%d attempt=%d body=%s",
			status, fileName, chunkIndex, attempt, strings.TrimSpace(string(body)))
		return
	}
	log.Printf("[audio] response status=%d file=%s mode=direct attempt=%d body=%s",
		status, fileName, attempt, strings.TrimSpace(string(body)))
}

func (c *AudioHTTPClient) waitForTranscriptionSlot(ctx context.Context) error {
	if c.requestInterval <= 0 {
		return nil
	}
	c.rateMu.Lock()
	now := time.Now()
	if c.nextRequestAt.Before(now) {
		c.nextRequestAt = now
	}
	slot := c.nextRequestAt
	c.nextRequestAt = c.nextRequestAt.Add(c.requestInterval)
	c.rateMu.Unlock()
	if wait := time.Until(slot); wait > 0 {
		return waitForRetry(ctx, wait)
	}
	return nil
}

func shouldRetryTranscription(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func transcriptionRetryDelay(retryAfter string, responseBody []byte, attempt int) time.Duration {
	if seconds, ok := parseRetryAfterSeconds(retryAfter); ok {
		return time.Duration(seconds) * time.Second
	}
	if seconds, ok := parseRetryAfterSeconds(string(responseBody)); ok {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(attempt+1) * 3 * time.Second
}

func parseRetryAfterSeconds(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if value, err := strconv.Atoi(raw); err == nil && value > 0 {
		return value, true
	}
	matches := regexp.MustCompile(`(?i)try again in (\d+)s`).FindStringSubmatch(raw)
	if len(matches) != 2 {
		return 0, false
	}
	value, err := strconv.Atoi(matches[1])
	return value, err == nil && value > 0
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
