package ingestion

import (
	"time"

	"gin-backend/model"
)

type ingestStats struct {
	StartedAt     time.Time
	ParseDuration time.Duration
	ChunkDuration time.Duration
	EmbedDuration time.Duration
	StoreDuration time.Duration
	TotalChunks   int
}

type producedChunk struct {
	Chunk model.Chunk
}
