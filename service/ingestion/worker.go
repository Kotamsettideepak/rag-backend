package ingestion

import (
	"context"
	"fmt"
	"log"
	"time"

	"gin-backend/model"
)

func (m *Manager) runJobWorker(ctx context.Context, workerID int) {
	defer m.wg.Done()
	log.Printf("[jobs] worker started id=%d", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		case queued, ok := <-m.jobQueue:
			if !ok {
				return
			}
			log.Printf("[jobs] picked job=%s worker=%d", queued.ID, workerID)
			m.processJob(ctx, queued)
		}
	}
}

func (m *Manager) processJob(parentCtx context.Context, queued queuedJob) {
	startedAt := time.Now().UTC()
	m.updateJob(queued.ID, func(j *model.UploadJob) {
		j.Status = model.JobProcessing
		j.Stage = "processing"
		j.Summary = stageLabel("processing")
		j.Detail = "Preparing files and selecting extraction pipeline."
		j.ProgressLabel = fmt.Sprintf("0 of %d files prepared", len(queued.Files))
		j.ProgressPercent = 5
		j.UpdatedAt = startedAt
		j.StartedAt = &startedAt
	})

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	defer m.parser.Cleanup(queued.Files)

	stats, err := m.runStreamingIngestion(ctx, cancel, queued)
	if err != nil {
		m.failJob(queued.ID, err)
		return
	}

	if stats.TotalChunks == 0 {
		m.completeJobEmpty(queued.ID, stats.StartedAt, stats.ParseDuration, stats.ChunkDuration)
		return
	}

	m.completeJob(queued.ID, queued.Files, stats.TotalChunks, stats.StartedAt, stats.ParseDuration, stats.ChunkDuration, stats.EmbedDuration, stats.StoreDuration)
}
