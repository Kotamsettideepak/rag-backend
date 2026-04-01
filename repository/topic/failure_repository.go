package topic

import (
	"context"
	"fmt"
	"strings"

	"gin-backend/repository"

	"github.com/google/uuid"
)

type FailedChunk struct {
	JobID      string
	TopicID    string
	FileID     string
	ChunkIndex int
	Reason     string
	Attempts   int
	Payload    string
}

func (r *Repository) SaveFailures(ctx context.Context, rows []FailedChunk) error {
	db := repository.DefaultGorm()
	if db == nil {
		return fmt.Errorf("database store is not initialized")
	}
	if len(rows) == 0 {
		return nil
	}

	records := make([]repository.TopicChunkFailure, 0, len(rows))
	for _, row := range rows {
		topicID := strings.TrimSpace(row.TopicID)
		fileID := strings.TrimSpace(row.FileID)
		reason := strings.TrimSpace(row.Reason)
		if topicID == "" || fileID == "" || reason == "" {
			continue
		}
		payload := strings.TrimSpace(row.Payload)
		if payload == "" {
			payload = "{}"
		}
		records = append(records, repository.TopicChunkFailure{
			ID:         uuid.NewString(),
			TopicID:    topicID,
			JobID:      strings.TrimSpace(row.JobID),
			FileID:     fileID,
			ChunkIndex: row.ChunkIndex,
			Reason:     reason,
			Attempts:   row.Attempts,
			Payload:    payload,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&records).Error
}
