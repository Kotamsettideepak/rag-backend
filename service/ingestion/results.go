package ingestion

import (
	"fmt"
	"log"
	"time"

	"gin-backend/model"
)

func (m *Manager) completeJob(jobID string, files []model.StagedFile, totalChunks int,
	start time.Time, parseDur, chunkDur, embedDur, storeDur time.Duration) {
	total := time.Since(start)
	throughput := float64(totalChunks)
	if total > 0 {
		throughput = throughput / total.Seconds()
	}

	completedAt := time.Now().UTC()
	m.updateJob(jobID, func(j *model.UploadJob) {
		j.Status = model.JobCompleted
		j.Stage = "completed"
		j.CompletedAt = &completedAt
		j.UpdatedAt = completedAt
		j.Summary = stageLabel("completed")
		j.Detail = fmt.Sprintf("Finished %d file(s). %d chunks indexed.", len(files), j.IndexedChunks)
		j.ChatReady = j.IndexedChunks > 0
		if j.ChatReady && j.ChatReadyAt == nil {
			j.ChatReadyAt = &completedAt
		}
		j.ProgressLabel = "Ready for chat"
		j.ProgressPercent = 100
		j.Metrics.ParseDurationMs = parseDur.Milliseconds()
		j.Metrics.ChunkDurationMs = chunkDur.Milliseconds()
		j.Metrics.EmbeddingDurationMs = embedDur.Milliseconds()
		j.Metrics.StorageDurationMs = storeDur.Milliseconds()
		j.Metrics.TotalDurationMs = total.Milliseconds()
		j.Metrics.ThroughputChunksSec = throughput
		for i := range j.Files {
			if j.Files[i].Status != "failed" {
				j.Files[i].Status = "completed"
			}
		}
	})
}

func (m *Manager) completeJobEmpty(jobID string, start time.Time, parseDur, chunkDur time.Duration) {
	completedAt := time.Now().UTC()
	m.updateJob(jobID, func(j *model.UploadJob) {
		j.Status = model.JobCompleted
		j.Stage = "completed"
		j.CompletedAt = &completedAt
		j.UpdatedAt = completedAt
		j.Summary = "Upload completed, but no extractable text was found."
		j.Detail = "The pipeline finished, but there was no usable text to index."
		j.ProgressLabel = "Nothing was indexed"
		j.ProgressPercent = 100
		j.Metrics.ParseDurationMs = parseDur.Milliseconds()
		j.Metrics.ChunkDurationMs = chunkDur.Milliseconds()
		j.Metrics.TotalDurationMs = time.Since(start).Milliseconds()
	})
}

func (m *Manager) markIndexed(jobID string, added int) {
	now := time.Now().UTC()
	m.updateJob(jobID, func(j *model.UploadJob) {
		j.IndexedChunks += added
		j.CompletedChunks = j.IndexedChunks
		if !j.ChatReady {
			j.ChatReady = true
			j.Status = model.JobChatReady
			j.ChatReadyAt = &now
			j.Summary = stageLabel("chat_ready")
			j.Detail = fmt.Sprintf("%d chunks are already searchable. You can start chatting while indexing continues.", j.IndexedChunks)
			j.ProgressLabel = "Chat ready while indexing continues"
			log.Printf("[submit-phase] job=%s first_flush_complete indexed_chunks=%d chat_ready=true", jobID, j.IndexedChunks)
		} else {
			j.Detail = fmt.Sprintf("%d of %d chunks are searchable while indexing continues.", j.IndexedChunks, maxInt(j.TotalChunks, j.IndexedChunks))
			j.ProgressLabel = "Indexing more chunks in the background"
			log.Printf("[submit-phase] job=%s additional_flush_complete indexed_chunks=%d total_chunks=%d", jobID, j.IndexedChunks, maxInt(j.TotalChunks, j.IndexedChunks))
		}
		if j.Stage != "completed" {
			j.Stage = "chat_ready"
		}
		j.ProgressPercent = clampPct(55 + (j.IndexedChunks * 40 / maxInt(j.TotalChunks, 1)))
		j.UpdatedAt = now
	})
}

func (m *Manager) failJob(jobID string, err error) {
	log.Printf("[ingest] job=%s failed: %v", jobID, err)
	completedAt := time.Now().UTC()
	m.updateJob(jobID, func(j *model.UploadJob) {
		j.Status = model.JobFailed
		j.Stage = "failed"
		j.Error = err.Error()
		j.Summary = stageLabel("failed")
		j.Detail = "The pipeline stopped. Check the error message for the exact reason."
		j.ProgressLabel = "Processing stopped"
		j.ProgressPercent = 100
		j.CompletedAt = &completedAt
		j.UpdatedAt = completedAt
		j.Metrics.TotalDurationMs = j.UpdatedAt.Sub(j.CreatedAt).Milliseconds()
	})
}
