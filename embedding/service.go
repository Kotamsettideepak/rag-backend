package embedding

import (
	"context"
	"fmt"
	"os"
	"strconv"
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
	records := make([]models.VectorRecord, 0, len(chunks))

	for _, chunk := range chunks {
		embedding, err := s.embedWithRetry(ctx, chunk.Text)
		if err != nil {
			return nil, err
		}

		records = append(records, models.VectorRecord{
			ID:        chunk.ID,
			Text:      chunk.Text,
			Embedding: embedding,
			Metadata: map[string]interface{}{
				"file_id":    chunk.FileID,
				"file_name":  chunk.FileName,
				"file_kind":  chunk.FileKind,
				"page":       chunk.Page,
				"chunk_idx":  chunk.Index,
				"chunk_hash": chunk.Hash,
				"source":     "upload",
			},
		})
	}

	return records, nil
}

func (s *Service) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return s.client.GenerateEmbeddingWithContext(ctx, text)
}

func (s *Service) embedWithRetry(ctx context.Context, text string) ([]float64, error) {
	var lastErr error
	for attempt := 1; attempt <= s.maxRetries; attempt++ {
		embedding, err := s.client.GenerateEmbeddingWithContext(ctx, text)
		if err == nil {
			return embedding, nil
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

	return nil, fmt.Errorf("embedding failed after %d attempts: %w", s.maxRetries, lastErr)
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
