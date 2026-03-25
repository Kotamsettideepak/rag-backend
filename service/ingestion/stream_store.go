package ingestion

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gin-backend/model"
	"gin-backend/pkg/worker"
)

func (m *Manager) runStreamingIngestion(parentCtx context.Context, cancel context.CancelFunc, queued queuedJob) (ingestStats, error) {
	chunkQueue := make(chan model.Chunk, m.chunkQueueSize)
	resultCh := make(chan worker.BatchResult, m.resultBufSize)
	producedCh := make(chan struct {
		stats ingestStats
		err   error
	}, 1)

	var submitWG sync.WaitGroup
	batcherDone := make(chan struct{})

	go func() {
		defer close(batcherDone)
		m.submitEmbedTasks(parentCtx, queued.ID, chunkQueue, resultCh, &submitWG)
	}()
	go func() {
		result := struct {
			stats ingestStats
			err   error
		}{}
		result.stats, result.err = m.produceChunks(parentCtx, queued, chunkQueue)
		producedCh <- result
		close(chunkQueue)
	}()
	go func() {
		<-batcherDone
		submitWG.Wait()
		close(resultCh)
	}()

	storeStats, storeErr := m.collectAndStore(parentCtx, cancel, queued.ID, resultCh)
	produced := <-producedCh

	stats := produced.stats
	stats.EmbedDuration = storeStats.EmbedDuration
	stats.StoreDuration = storeStats.StoreDuration

	if produced.err != nil && produced.err != context.Canceled {
		return stats, produced.err
	}
	if storeErr != nil {
		return stats, storeErr
	}
	if parentCtx.Err() != nil && parentCtx.Err() != context.Canceled {
		return stats, parentCtx.Err()
	}

	return stats, nil
}

func (m *Manager) collectAndStore(ctx context.Context, cancel context.CancelFunc, jobID string, results <-chan worker.BatchResult) (ingestStats, error) {
	stats := ingestStats{}
	pending := make([]model.VectorRecord, 0, m.storeBatchSize)
	firstErr := error(nil)
	completedBatches := 0

	for result := range results {
		stats.EmbedDuration += result.Duration
		if result.Err != nil && firstErr == nil {
			firstErr = result.Err
			cancel()
		}
		if result.Err != nil {
			log.Printf("[submit-phase] job=%s embed_batch_failed processed=%d err=%v", jobID, result.Processed, result.Err)
			continue
		}

		completedBatches++
		if completedBatches <= 3 || completedBatches%10 == 0 || len(pending)+len(result.Records) >= m.storeBatchSize {
			log.Printf("[submit-phase] job=%s completed_batches=%d batch_chunks=%d pending_vectors=%d", jobID, completedBatches, result.Processed, len(pending)+len(result.Records))
		}
		pending = append(pending, result.Records...)
		if len(pending) < m.storeBatchSize {
			continue
		}
		if err := m.flushRecords(ctx, jobID, pending, &stats); err != nil && firstErr == nil {
			firstErr = err
			cancel()
		}
		pending = pending[:0]
	}

	if firstErr != nil {
		return stats, firstErr
	}
	if len(pending) == 0 {
		return stats, nil
	}
	return stats, m.flushRecords(ctx, jobID, pending, &stats)
}

func (m *Manager) flushRecords(ctx context.Context, jobID string, records []model.VectorRecord, stats *ingestStats) error {
	if len(records) == 0 {
		return nil
	}

	m.setJobStage(jobID, "storing")
	log.Printf("[submit-phase] job=%s flush_start records=%d", jobID, len(records))
	storeStart := time.Now()
	if err := m.store.AddRecords(records); err != nil {
		return fmt.Errorf("vector store failed: %w", err)
	}
	stats.StoreDuration += time.Since(storeStart)
	log.Printf("[submit-phase] job=%s flush_done records=%d elapsed_ms=%d", jobID, len(records), time.Since(storeStart).Milliseconds())
	m.markIndexed(jobID, len(records))
	return nil
}
