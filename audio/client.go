package audio

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
	"strings"

	"gin-backend/config"
	"gin-backend/models"
)

type Client interface {
	Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error)
}

type HTTPClient struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

type transcriptionResponse struct {
	Text     string               `json:"text"`
	Duration float64              `json:"duration"`
	Segments []transcribedSegment `json:"segments"`
}

type transcribedSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func NewHTTPClient() Client {
	return &HTTPClient{
		baseURL: config.GetGroqBaseURL(),
		model:   config.GetGroqAudioModel(),
		apiKey:  config.GetGroqAPIKey(),
		client:  &http.Client{Timeout: config.GetGroqTimeout()},
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return models.ParsedDocument{}, fmt.Errorf("GROQ_API_KEY is not configured")
	}

	fileData, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	if len(fileData) > 25*1024*1024 {
		return models.ParsedDocument{}, fmt.Errorf("audio file exceeds current transcription limit of 25 MB")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(staged.OriginalName))
	if err != nil {
		return models.ParsedDocument{}, err
	}
	if _, err := part.Write(fileData); err != nil {
		return models.ParsedDocument{}, err
	}
	if err := writer.WriteField("model", c.model); err != nil {
		return models.ParsedDocument{}, err
	}
	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return models.ParsedDocument{}, err
	}
	if err := writer.WriteField("timestamp_granularities[]", "segment"); err != nil {
		return models.ParsedDocument{}, err
	}
	if err := writer.Close(); err != nil {
		return models.ParsedDocument{}, err
	}

	endpoint := c.baseURL + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	log.Printf(
		"[audio] sending Groq transcription request url=%s filename=%s model=%s bytes=%d",
		endpoint,
		filepath.Base(staged.OriginalName),
		c.model,
		len(fileData),
	)

	resp, err := c.client.Do(req)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	log.Printf("[audio] response status=%d file=%s body=%s", resp.StatusCode, staged.OriginalName, strings.TrimSpace(string(responseBody)))
	if resp.StatusCode != http.StatusOK {
		return models.ParsedDocument{}, fmt.Errorf("groq transcription failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed transcriptionResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return models.ParsedDocument{}, err
	}

	documentText, pageTexts := buildAudioTexts(staged, parsed)

	return models.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  staged.DetectedKind,
		Text:      documentText,
		PageTexts: pageTexts,
	}, nil
}

func buildAudioTexts(staged models.StagedFile, response transcriptionResponse) (string, []string) {
	metadata := buildAudioMetadataBlock(staged, response)

	parts := []string{metadata}
	pageTexts := make([]string, 0, maxInt(1, len(response.Segments)+1))
	pageTexts = append(pageTexts, metadata)
	for _, segment := range response.Segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}

		entry := fmt.Sprintf("[%.2fs - %.2fs] %s", segment.Start, segment.End, text)
		parts = append(parts, entry)
		pageTexts = append(pageTexts, entry)
	}

	if len(pageTexts) == 0 {
		pageTexts = []string{metadata}
	}

	text := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if text == "" {
		text = metadata
	}

	return text, pageTexts
}

func buildAudioMetadataBlock(staged models.StagedFile, response transcriptionResponse) string {
	durationLine := "Estimated duration: unavailable"
	if duration := estimateAudioDuration(response); duration > 0 {
		durationLine = fmt.Sprintf("Estimated duration: %.2f seconds", duration)
	}

	return strings.TrimSpace(strings.Join([]string{
		"Uploaded Audio Metadata",
		"Actual uploaded filename: " + strings.TrimSpace(staged.OriginalName),
		"Detected file type: " + strings.ToUpper(strings.TrimSpace(staged.DetectedKind)),
		"Content-Type: " + strings.TrimSpace(staged.ContentType),
		durationLine,
	}, "\n"))
}

func estimateAudioDuration(response transcriptionResponse) float64 {
	if response.Duration > 0 {
		return response.Duration
	}
	var maxEnd float64
	for _, segment := range response.Segments {
		if segment.End > maxEnd {
			maxEnd = segment.End
		}
	}
	return maxEnd
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
