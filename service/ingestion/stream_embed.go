package ingestion

import (
	"context"
	"log"
	"sync"

	"gin-backend/model"
	"gin-backend/pkg/worker"
)

func (m *Manager) submitEmbedTasks(ctx context.Context, jobID string, chunks <-chan model.Chunk, results chan worker.BatchResult, wg *sync.WaitGroup) {
	batch := make([]model.Chunk, 0, m.embedPolicy.MaxChunks)
	chars := 0
	bytes := 0
	submittedBatches := 0

	flush := func() {
		if len(batch) == 0 || ctx.Err() != nil {
			return
		}
		payload := append([]model.Chunk(nil), batch...)
		wg.Add(1)
		m.setJobStage(jobID, "embedding")
		submittedBatches++
		if submittedBatches <= 3 || submittedBatches%10 == 0 || len(payload) < m.embedPolicy.MaxChunks {
			log.Printf("[submit-phase] job=%s submitted_batches=%d batch_chunks=%d chars=%d bytes=%d", jobID, submittedBatches, len(payload), chars, bytes)
		}
		m.pool.Submit(worker.BatchTask{Ctx: ctx, JobID: jobID, Batch: payload, Response: results})
		batch = batch[:0]
		chars = 0
		bytes = 0
	}

	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-chunks:
			if !ok {
				flush()
				return
			}
			if !m.embedPolicy.canAppend(batch, chars, bytes, chunk) {
				flush()
			}
			batch = append(batch, chunk)
			chars += len(chunk.Text)
			bytes += len([]byte(chunk.Text))
		}
	}
}
