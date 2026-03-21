package embedding

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gin-backend/models"
)

type Service struct {
	client       *models.OllamaClient
	maxRetries   int
	retryBackoff time.Duration
}

func NewService(client *models.OllamaClient) *Service {
	return &Service{
		client:       client,
		maxRetries:   getEnvInt("EMBED_MAX_RETRIES", 3),
		retryBackoff: time.Duration(getEnvInt("EMBED_RETRY_BACKOFF_MS", 400)) * time.Millisecond,
	}
}

func (s *Service) EmbedChunks(ctx context.Context, chunks []models.Chunk) ([]models.VectorRecord, error) {
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

	records := make([]models.VectorRecord, 0, len(chunks))
	for index, chunk := range chunks {
		embedding := embeddings[index]

		records = append(records, models.VectorRecord{
			ID:        chunk.ID,
			Text:      chunk.Text,
			Embedding: embedding,
			Metadata:  buildChunkMetadata(chunk),
		})
	}

	return records, nil
}

func buildChunkMetadata(chunk models.Chunk) map[string]interface{} {
	metadata := map[string]interface{}{
		"file_id":    chunk.FileID,
		"file_name":  chunk.FileName,
		"file_kind":  chunk.FileKind,
		"page":       chunk.Page,
		"chunk_idx":  chunk.Index,
		"chunk_hash": chunk.Hash,
		"source":     "upload",
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
	return s.client.GenerateEmbeddingWithContext(ctx, text)
}

func (s *Service) embedBatchWithRetry(ctx context.Context, texts []string) ([][]float64, error) {
	var lastErr error
	for attempt := 1; attempt <= s.maxRetries; attempt++ {
		embeddings, err := s.client.GenerateEmbeddingsWithContext(ctx, texts)
		if err == nil {
			return embeddings, nil
		}

		lastErr = err

		if attempt == s.maxRetries {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.retryBackoff * time.Duration(attempt)):
		}
	}

	return nil, fmt.Errorf("batch embedding failed after %d attempts: %w", s.maxRetries, lastErr)
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
