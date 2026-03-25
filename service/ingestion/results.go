package ingestion

import (
	"fmt"
	"log"
	"time"

	"gin-backend/model"
)

func (m *Manager) completeJob(jobID string, files []model.StagedFile, chunks []model.Chunk,
	start time.Time, parseDur, chunkDur, embedDur, storeDur time.Duration) {
	total := time.Since(start)
	throughput := float64(len(chunks))
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
		j.Detail = fmt.Sprintf("Finished %d file(s). %d chunks indexed.", len(files), len(chunks))
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
