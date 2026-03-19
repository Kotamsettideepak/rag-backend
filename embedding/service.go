package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"gin-backend/models"
)

type Service struct {
	client       *models.OllamaClient
	cache        *Cache
	maxRetries   int
	retryBackoff time.Duration
}

func NewService(client *models.OllamaClient, cache *Cache) *Service {
	return &Service{
		client:       client,
		cache:        cache,
		maxRetries:   getEnvInt("EMBED_MAX_RETRIES", 3),
		retryBackoff: time.Duration(getEnvInt("EMBED_RETRY_BACKOFF_MS", 400)) * time.Millisecond,
	}
}

func (s *Service) EmbedChunks(ctx context.Context, chunks []models.Chunk) ([]models.VectorRecord, int, error) {
	records := make([]models.VectorRecord, 0, len(chunks))
	cacheHits := 0

	for _, chunk := range chunks {
		embedding, fromCache, err := s.embedWithRetry(ctx, chunk.Hash, chunk.Text)
		if err != nil {
			return nil, cacheHits, err
		}
		if fromCache {
			cacheHits++
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

	return records, cacheHits, nil
}

func (s *Service) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return s.client.GenerateEmbeddingWithContext(ctx, text)
}

func (s *Service) embedWithRetry(ctx context.Context, hash string, text string) ([]float64, bool, error) {
	if hash != "" {
		if cached, ok := s.cache.Get(hash); ok {
			return cached, true, nil
		}
	}

	var lastErr error
	for attempt := 1; attempt <= s.maxRetries; attempt++ {
		embedding, err := s.client.GenerateEmbeddingWithContext(ctx, text)
		if err == nil {
			if hash != "" {
				s.cache.Set(hash, embedding)
			}
			return embedding, false, nil
		}

		lastErr = err
		log.Printf("[embedding] attempt %d/%d failed: %v", attempt, s.maxRetries, err)

		if attempt == s.maxRetries {
			break
		}

		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(s.retryBackoff * time.Duration(attempt)):
		}
	}

	return nil, false, fmt.Errorf("embedding failed after %d attempts: %w", s.maxRetries, lastErr)
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
