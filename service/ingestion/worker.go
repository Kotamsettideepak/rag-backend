package ingestion

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gin-backend/model"
	"gin-backend/pkg/trace"
	"gin-backend/pkg/worker"
	uploadrepo "gin-backend/repository/upload"
)

const maxPDFPages = 300

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
	trace.Start("INGEST", "job="+queued.ID)
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

	processStart := time.Now()
	parseStart := time.Now()
	documents, parseOk := m.extractDocuments(ctx, queued)
	parseDuration := time.Since(parseStart)
	if !parseOk {
		return
	}

	if err := m.attachAndRecordUploads(ctx, queued); err != nil {
		trace.End("INGEST", "record failed")
		m.failJob(queued.ID, err)
		return
	}

	chunkStart := time.Now()
	allChunks := m.chunkDocuments(queued.ID, documents)
	chunkDuration := time.Since(chunkStart)

	if len(allChunks) == 0 {
		m.completeJobEmpty(queued.ID, processStart, parseDuration, chunkDuration)
		return
	}

	embedDur, storeDur, ok := m.embedAndStore(ctx, queued.ID, allChunks)
	if !ok {
		return
	}

	m.completeJob(queued.ID, queued.Files, allChunks, processStart, parseDuration, chunkDuration, embedDur, storeDur)
	trace.End("INGEST", fmt.Sprintf("job=%s chunks=%d", queued.ID, len(allChunks)))
}

func (m *Manager) extractDocuments(ctx context.Context, queued queuedJob) ([]model.ParsedDocument, bool) {
	documents := make([]model.ParsedDocument, 0, len(queued.Files))
	for i, file := range queued.Files {
		stop := func() {}
		if file.DetectedKind == "video" {
			stop = m.startVideoProgress(queued.ID, file, i, len(queued.Files))
		} else {
			m.setJobStageDetailed(queued.ID, "extracting", file, i, len(queued.Files), 18)
		}

		doc, err := m.router.Extract(ctx, file)
		stop()
		if err != nil {
			m.failJob(queued.ID, fmt.Errorf("extract failed for %s: %w", file.OriginalName, err))
			return nil, false
		}
		if err := validateDocumentLimits(doc); err != nil {
			m.failJob(queued.ID, err)
			return nil, false
		}
		documents = append(documents, doc)

		m.updateFile(queued.ID, file.FileID, func(r *model.FileResult) {
			r.Status = "parsed"
			r.Pages = len(doc.PageTexts)
		})
		m.updateJob(queued.ID, func(j *model.UploadJob) {
			j.CurrentFile = file.OriginalName
			j.CurrentKind = file.DetectedKind
			j.ProgressLabel = fmt.Sprintf("Extracted %d of %d files", i+1, len(queued.Files))
			j.ProgressPercent = clampPct(10 + ((i + 1) * 25 / maxInt(len(queued.Files), 1)))
			j.UpdatedAt = time.Now().UTC()
		})
	}
	return documents, true
}

func (m *Manager) attachAndRecordUploads(ctx context.Context, queued queuedJob) error {
	for i := range queued.Files {
		if err := m.parser.AttachCloudURL(ctx, &queued.Files[i]); err != nil {
			return fmt.Errorf("cloud upload failed for %s: %w", queued.Files[i].OriginalName, err)
		}
	}
	pg := uploadrepo.Default()
	if pg == nil {
		return fmt.Errorf("database store is not initialized")
	}
	for _, f := range queued.Files {
		url := strings.TrimSpace(f.CloudURL)
		if url == "" {
			url = strings.TrimSpace(f.SourceURL)
		}
		if url == "" {
			continue
		}
		if _, err := pg.Create(ctx, f.ChatID, url, f.DetectedKind, f.OriginalName); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) chunkDocuments(jobID string, documents []model.ParsedDocument) []model.Chunk {
	all := make([]model.Chunk, 0)
	m.setJobStage(jobID, "chunking")
	for i, doc := range documents {
		chunks := m.chunker.ChunkDocument(doc)
		all = append(all, chunks...)
		m.updateFile(jobID, doc.FileID, func(r *model.FileResult) { r.Status = "chunked" })
		m.updateJob(jobID, func(j *model.UploadJob) {
			j.ProgressLabel = fmt.Sprintf("Chunked %d of %d files into %d chunks", i+1, len(documents), len(all))
			j.ProgressPercent = clampPct(40 + ((i + 1) * 15 / maxInt(len(documents), 1)))
			j.UpdatedAt = time.Now().UTC()
		})
	}
	return all
}

func (m *Manager) embedAndStore(ctx context.Context, jobID string, allChunks []model.Chunk) (time.Duration, time.Duration, bool) {
	batches := splitBatches(allChunks, m.batchSize)
	m.setJobStage(jobID, "embedding")
	resultCh := make(chan worker.BatchResult, len(batches))
	for _, batch := range batches {
		m.pool.Submit(worker.BatchTask{Ctx: ctx, JobID: jobID, Batch: batch, Response: resultCh})
	}

	var embedDur, storeDur time.Duration
	pending := make([]model.VectorRecord, 0, m.storeBatchSize)
	pendingCount := 0

	for range batches {
		result := <-resultCh
		embedDur += result.Duration
		if result.Err != nil {
			m.failJob(jobID, result.Err)
			return 0, 0, false
		}
		pending = append(pending, result.Records...)
		pendingCount += result.Processed

		if len(pending) >= m.storeBatchSize {
			m.setJobStage(jobID, "storing")
			storeStart := time.Now()
			if err := m.store.AddRecords(pending); err != nil {
				m.failJob(jobID, fmt.Errorf("vector store failed: %w", err))
				return 0, 0, false
			}
			storeDur += time.Since(storeStart)
			m.updateJob(jobID, func(j *model.UploadJob) {
				j.CompletedChunks += pendingCount
				j.ProgressPercent = clampPct(55 + (j.CompletedChunks * 25 / maxInt(j.TotalChunks, 1)))
				j.UpdatedAt = time.Now().UTC()
			})
			pending = pending[:0]
			pendingCount = 0
		}
	}

	if len(pending) > 0 {
		m.setJobStage(jobID, "storing")
		storeStart := time.Now()
		if err := m.store.AddRecords(pending); err != nil {
			m.failJob(jobID, fmt.Errorf("vector store failed: %w", err))
			return 0, 0, false
		}
		storeDur += time.Since(storeStart)
		m.updateJob(jobID, func(j *model.UploadJob) {
			j.CompletedChunks += pendingCount
			j.UpdatedAt = time.Now().UTC()
		})
	}
	return embedDur, storeDur, true
}
