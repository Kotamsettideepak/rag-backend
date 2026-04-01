package embedding

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gin-backend/model"
)

type Service struct {
	client       Repository
	maxRetries   int
	retryBackoff time.Duration
}

func NewService(client Repository) *Service {
	return &Service{
		client:       client,
		maxRetries:   getEnvInt("EMBED_MAX_RETRIES", 3),
		retryBackoff: time.Duration(getEnvInt("EMBED_RETRY_BACKOFF_MS", 400)) * time.Millisecond,
	}
}

func (s *Service) EmbedChunks(ctx context.Context, chunks []model.Chunk) ([]model.VectorRecord, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	inputs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		inputs = append(inputs, chunk.Text)
	}

	embeddings, err := s.embedBatchWithRetry(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(embeddings) != len(chunks) {
		return nil, fmt.Errorf("embedding count mismatch: got %d embeddings for %d chunks", len(embeddings), len(chunks))
	}

	records := make([]model.VectorRecord, 0, len(chunks))
	for index, chunk := range chunks {
		embedding := embeddings[index]

		records = append(records, model.VectorRecord{
			ID:        chunk.ID,
			Text:      chunk.Text,
			Embedding: embedding,
			Metadata:  buildChunkMetadata(chunk),
		})
	}

	return records, nil
}

func buildChunkMetadata(chunk model.Chunk) map[string]interface{} {
	metadata := map[string]interface{}{
		"file_id":     chunk.FileID,
		"file_name":   chunk.FileName,
		"file_kind":   chunk.FileKind,
		"chat_id":     chunk.ChatID,
		"user_id":     chunk.UserID,
		"topic_id":    chunk.TopicID,
		"page":        chunk.Page,
		"chunk_idx":   chunk.Index,
		"chunk_index": chunk.Index,
		"chunk_hash":  chunk.Hash,
		"hash":        chunk.Hash,
		"source":      "upload",
	}
	if strings.TrimSpace(chunk.TopicID) != "" && strings.TrimSpace(chunk.ChatID) == "" {
		metadata["source"] = "topic"
	}

	for key, value := range chunk.Metadata {
		metadata[key] = value
	}

	if chunk.FileKind == "image" {
		switch {
		case strings.HasPrefix(chunk.Text, "Uploaded Image Metadata"):
			metadata["content_type"] = "image_metadata"
		default:
			metadata["content_type"] = "image_analysis"
		}
		return metadata
	}

	if chunk.FileKind == "video" {
		if strings.HasPrefix(chunk.Text, "Uploaded Video Metadata") {
			metadata["content_type"] = "video_metadata"
			if duration, ok := parseEstimatedDuration(chunk.Text); ok {
				metadata["estimated_duration_sec"] = duration
			}
			return metadata
		}
		if _, exists := metadata["content_type"]; exists {
			return metadata
		}
		metadata["content_type"] = "video_transcript"
		if start, end, ok := parseSegmentRange(chunk.Text); ok {
			metadata["segment_start"] = start
			metadata["segment_end"] = end
		}
		return metadata
	}

	if chunk.FileKind != "audio" {
		if _, exists := metadata["content_type"]; !exists {
			metadata["content_type"] = "document"
		}
		return metadata
	}

	if strings.HasPrefix(chunk.Text, "Uploaded Audio Metadata") {
		metadata["content_type"] = "audio_metadata"
		if duration, ok := parseEstimatedDuration(chunk.Text); ok {
			metadata["estimated_duration_sec"] = duration
		}
		return metadata
	}

	if _, exists := metadata["content_type"]; exists {
		return metadata
	}

	metadata["content_type"] = "audio_transcript"
	if start, end, ok := parseSegmentRange(chunk.Text); ok {
		metadata["segment_start"] = start
		metadata["segment_end"] = end
	}

	return metadata
}

func (s *Service) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	embeddings, err := s.client.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) != 1 {
		return nil, fmt.Errorf("embedding repository returned %d embeddings for a single input", len(embeddings))
	}
	return toFloat64Vector(embeddings[0]), nil
}

func (s *Service) embedBatchWithRetry(ctx context.Context, texts []string) ([][]float64, error) {
	embeddings, err := s.embedAdaptive(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("batch embedding failed: %w", err)
	}
	return toFloat64Vectors(embeddings), nil
}

func (s *Service) embedAdaptive(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings, err := s.embedWithRetries(ctx, texts)
	if err == nil {
		return embeddings, nil
	}
	if len(texts) == 1 || !shouldSplitBatch(err) {
		return nil, err
	}

	mid := len(texts) / 2
	left, err := s.embedAdaptive(ctx, texts[:mid])
	if err != nil {
		return nil, err
	}
	right, err := s.embedAdaptive(ctx, texts[mid:])
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func (s *Service) embedWithRetries(ctx context.Context, texts []string) ([][]float32, error) {
	var lastErr error
	for attempt := 1; attempt <= s.maxRetries; attempt++ {
		embeddings, err := s.client.Embed(ctx, texts)
		if err == nil {
			return embeddings, nil
		}

		lastErr = err
		if attempt == s.maxRetries {
			break
		}

		delay := s.retryBackoff * time.Duration(attempt)
		if isRateLimitError(err) {
			delay *= 2
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

func shouldSplitBatch(err error) bool {
	return isPayloadLimitError(err) || isRateLimitError(err)
}

func isPayloadLimitError(err error) bool {
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "payload") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "input too large") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "request too large") ||
		strings.Contains(lower, "status 413")
}

func isRateLimitError(err error) bool {
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "status 429")
}

func toFloat64Vectors(vectors [][]float32) [][]float64 {
	if len(vectors) == 0 {
		return nil
	}

	converted := make([][]float64, 0, len(vectors))
	for _, vector := range vectors {
		converted = append(converted, toFloat64Vector(vector))
	}
	return converted
}

func toFloat64Vector(vector []float32) []float64 {
	converted := make([]float64, len(vector))
	for index, value := range vector {
		converted[index] = float64(value)
	}
	return converted
}

func getEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}

func parseEstimatedDuration(text string) (float64, bool) {
	const prefix = "Estimated duration: "
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(line, prefix), " seconds")
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil && parsed > 0 {
			return parsed, true
		}
	}
	return 0, false
}

func parseSegmentRange(text string) (float64, float64, bool) {
	if !strings.HasPrefix(text, "[") {
		return 0, 0, false
	}
	endBracket := strings.Index(text, "]")
	if endBracket <= 1 {
		return 0, 0, false
	}

	rangeText := strings.TrimSpace(text[1:endBracket])
	parts := strings.Split(rangeText, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}

	startRaw := strings.TrimSuffix(strings.TrimSpace(parts[0]), "s")
	endRaw := strings.TrimSuffix(strings.TrimSpace(parts[1]), "s")
	start, err := strconv.ParseFloat(startRaw, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err := strconv.ParseFloat(endRaw, 64)
	if err != nil {
		return 0, 0, false
	}

	return start, end, true
}
