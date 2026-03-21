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
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gin-backend/config"
	"gin-backend/models"
)

const (
	audioChunkSizeSeconds    = 25.0
	audioChunkOverlapSeconds = 5.0
	audioChunkStepSeconds    = audioChunkSizeSeconds - audioChunkOverlapSeconds
	minTranscriptChars       = 20
	minMergedChunkChars      = 50
	maxTranscriptionBytes    = 25 * 1024 * 1024
)

type Client interface {
	Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error)
}

type HTTPClient struct {
	baseURL         string
	model           string
	apiKey          string
	client          *http.Client
	requestInterval time.Duration
	maxRetries      int
	rateMu          sync.Mutex
	nextRequestAt   time.Time
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

type audioWindow struct {
	Start float64
	End   float64
}

func NewHTTPClient() Client {
	return &HTTPClient{
		baseURL:         config.GetGroqBaseURL(),
		model:           config.GetGroqAudioModel(),
		apiKey:          config.GetGroqAPIKey(),
		client:          &http.Client{Timeout: config.GetGroqTimeout()},
		requestInterval: config.GetGroqAudioRequestInterval(),
		maxRetries:      config.GetGroqAudioMaxRetries(),
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return models.ParsedDocument{}, fmt.Errorf("GROQ_API_KEY is not configured")
	}

	log.Printf(
		"[audio] preparing transcription file=%s kind=%s content_type=%s path=%s model=%s",
		staged.OriginalName,
		staged.DetectedKind,
		staged.ContentType,
		staged.StoredPath,
		c.model,
	)

	duration, err := probeAudioDuration(ctx, staged.StoredPath)
	if err != nil {
		return models.ParsedDocument{}, err
	}

	windows := buildAudioWindows(duration)
	if len(windows) == 0 {
		return models.ParsedDocument{}, fmt.Errorf("audio duration is unavailable")
	}

	chunks, err := c.transcribeAudioChunks(ctx, staged, windows)
	if err != nil {
		return models.ParsedDocument{}, err
	}

	mergedChunks := mergeSmallTranscriptChunks(chunks)
	documentText, pageTexts := buildAudioTexts(staged, duration, mergedChunks)

	log.Printf(
		"[audio] extracted file=%s duration=%.2fs windows=%d kept_chunks=%d text_chars=%d preview=%s",
		staged.OriginalName,
		duration,
		len(windows),
		len(mergedChunks),
		len(documentText),
		previewText(documentText, 220),
	)

	return models.ParsedDocument{
		FileID:      staged.FileID,
		FileName:    staged.OriginalName,
		FileKind:    staged.DetectedKind,
		Text:        documentText,
		PageTexts:   pageTexts,
		AudioChunks: mergedChunks,
	}, nil
}

func (c *HTTPClient) transcribeAudioChunks(
	ctx context.Context,
	staged models.StagedFile,
	windows []audioWindow,
) ([]models.AudioTranscriptChunk, error) {
	tempDir, err := os.MkdirTemp("", "audio-rag-chunks-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	chunks := make([]models.AudioTranscriptChunk, 0, len(windows))
	for index, window := range windows {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk-%03d.mp3", index))
		if err := splitAudioChunk(ctx, staged.StoredPath, chunkPath, window); err != nil {
			return nil, err
		}

		response, err := c.transcribeChunkFile(ctx, chunkPath, staged.OriginalName, index)
		if err != nil {
			return nil, err
		}

		chunk, ok := buildTranscriptChunk(window, response)
		if !ok {
			continue
		}

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

func (c *HTTPClient) transcribeChunkFile(
	ctx context.Context,
	chunkPath string,
	originalName string,
	chunkIndex int,
) (transcriptionResponse, error) {
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

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(chunkPath))
	if err != nil {
		return transcriptionResponse{}, err
	}
	if _, err := part.Write(fileData); err != nil {
		return transcriptionResponse{}, err
	}
	if err := writer.WriteField("model", c.model); err != nil {
		return transcriptionResponse{}, err
	}
	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return transcriptionResponse{}, err
	}
	if err := writer.WriteField("timestamp_granularities[]", "segment"); err != nil {
		return transcriptionResponse{}, err
	}
	if err := writer.Close(); err != nil {
		return transcriptionResponse{}, err
	}

	endpoint := c.baseURL + "/audio/transcriptions"
	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		if err := c.waitForTranscriptionSlot(ctx); err != nil {
			return transcriptionResponse{}, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body.Bytes()))
		if err != nil {
			return transcriptionResponse{}, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		log.Printf(
			"[audio] sending Groq transcription request url=%s file=%s chunk=%d attempt=%d model=%s bytes=%d",
			endpoint,
			filepath.Base(originalName),
			chunkIndex,
			attempt,
			c.model,
			len(fileData),
		)

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
		log.Printf(
			"[audio] response status=%d file=%s chunk=%d attempt=%d body=%s",
			resp.StatusCode,
			originalName,
			chunkIndex,
			attempt,
			strings.TrimSpace(string(responseBody)),
		)
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

func buildAudioTexts(
	staged models.StagedFile,
	duration float64,
	chunks []models.AudioTranscriptChunk,
) (string, []string) {
	metadata := buildAudioMetadataBlock(staged, duration)

	parts := []string{metadata}
	pageTexts := []string{metadata}
	for _, chunk := range chunks {
		entry := formatAudioTranscriptLine(chunk)
		if entry == "" {
			continue
		}

		parts = append(parts, entry)
		pageTexts = append(pageTexts, entry)
	}

	text := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if text == "" {
		text = metadata
	}

	return text, pageTexts
}

func buildAudioMetadataBlock(staged models.StagedFile, duration float64) string {
	durationLine := "Estimated duration: unavailable"
	if duration > 0 {
		durationLine = fmt.Sprintf("Estimated duration: %.2f seconds", duration)
	}

	lines := []string{
		"Uploaded Audio Metadata",
		"Actual uploaded filename: " + strings.TrimSpace(staged.OriginalName),
		"Detected file type: " + strings.ToUpper(strings.TrimSpace(staged.DetectedKind)),
		"Content-Type: " + strings.TrimSpace(staged.ContentType),
		durationLine,
	}
	if strings.TrimSpace(staged.SourceURL) != "" {
		lines = append(lines, "Source URL: "+strings.TrimSpace(staged.SourceURL))
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildAudioWindows(duration float64) []audioWindow {
	if duration <= 0 {
		return nil
	}

	windows := make([]audioWindow, 0, int(duration/audioChunkStepSeconds)+1)
	for start := 0.0; start < duration; start += audioChunkStepSeconds {
		end := start + audioChunkSizeSeconds
		if end > duration {
			end = duration
		}
		if end-start <= 0 {
			continue
		}

		windows = append(windows, audioWindow{
			Start: start,
			End:   end,
		})

		if end >= duration {
			break
		}
	}

	return windows
}

func splitAudioChunk(ctx context.Context, inputPath string, outputPath string, window audioWindow) error {
	command := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-y",
		"-i", inputPath,
		"-ss", formatTimestamp(window.Start),
		"-to", formatTimestamp(window.End),
		"-c", "copy",
		outputPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg chunk split failed for %.2f-%.2f seconds: %w: %s", window.Start, window.End, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func probeAudioDuration(ctx context.Context, path string) (float64, error) {
	command := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nokey=1:noprint_wrappers=1",
		path,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration probe failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid ffprobe duration output: %s", strings.TrimSpace(string(output)))
	}

	return duration, nil
}

func buildTranscriptChunk(window audioWindow, response transcriptionResponse) (models.AudioTranscriptChunk, bool) {
	if len(response.Segments) == 0 {
		return models.AudioTranscriptChunk{}, false
	}

	texts := make([]string, 0, len(response.Segments))
	start := -1.0
	end := 0.0

	for _, segment := range response.Segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}

		texts = append(texts, text)
		if start < 0 || segment.Start < start {
			start = segment.Start
		}
		if segment.End > end {
			end = segment.End
		}
	}

	if len(texts) == 0 {
		return models.AudioTranscriptChunk{}, false
	}

	fullText := strings.Join(texts, " ")
	fullText = strings.Join(strings.Fields(strings.TrimSpace(fullText)), " ")
	if len(fullText) < minTranscriptChars {
		return models.AudioTranscriptChunk{}, false
	}

	absoluteStart := window.Start
	if start >= 0 {
		absoluteStart += start
	}
	absoluteEnd := window.End
	if end > 0 {
		absoluteEnd = window.Start + end
	}
	if absoluteEnd < absoluteStart {
		absoluteEnd = absoluteStart
	}

	return models.AudioTranscriptChunk{
		Content: fullText,
		Start:   absoluteStart,
		End:     absoluteEnd,
		Type:    "audio_transcript",
	}, true
}

func mergeSmallTranscriptChunks(chunks []models.AudioTranscriptChunk) []models.AudioTranscriptChunk {
	if len(chunks) == 0 {
		return nil
	}

	merged := make([]models.AudioTranscriptChunk, 0, len(chunks))
	var pending *models.AudioTranscriptChunk

	for _, chunk := range chunks {
		chunk.Content = strings.TrimSpace(chunk.Content)
		if chunk.Content == "" {
			continue
		}

		if pending != nil {
			chunk.Content = strings.TrimSpace(pending.Content + " " + chunk.Content)
			chunk.Start = pending.Start
			if chunk.Type == "" {
				chunk.Type = pending.Type
			}
			pending = nil
		}

		if len(chunk.Content) < minMergedChunkChars {
			copyChunk := chunk
			pending = &copyChunk
			continue
		}

		merged = append(merged, chunk)
	}

	if pending != nil {
		if len(merged) == 0 {
			merged = append(merged, *pending)
		} else {
			lastIndex := len(merged) - 1
			merged[lastIndex].Content = strings.TrimSpace(merged[lastIndex].Content + " " + pending.Content)
			if pending.End > merged[lastIndex].End {
				merged[lastIndex].End = pending.End
			}
		}
	}

	return merged
}

func formatAudioTranscriptLine(chunk models.AudioTranscriptChunk) string {
	text := strings.TrimSpace(chunk.Content)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("[%s - %s] %s", formatTimestamp(chunk.Start), formatTimestamp(chunk.End), text)
}

func formatTimestamp(seconds float64) string {
	return fmt.Sprintf("%.2f", seconds)
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func (c *HTTPClient) waitForTranscriptionSlot(ctx context.Context) error {
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

	wait := time.Until(slot)
	if wait <= 0 {
		return nil
	}

	return waitForRetry(ctx, wait)
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
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
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
