package ingestion

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gin-backend/model"
	uploadrepo "gin-backend/repository/upload"
)

func (m *Manager) produceChunks(ctx context.Context, queued queuedJob, chunkQueue chan<- model.Chunk) (ingestStats, error) {
	stats := ingestStats{StartedAt: time.Now()}
	queuedChunks := 0
	pg := uploadrepo.Default()
	if pg == nil {
		return stats, fmt.Errorf("database store is not initialized")
	}

	for i, file := range queued.Files {
		parseStart := time.Now()
		doc, err := m.extractDocument(ctx, queued.ID, file, i, len(queued.Files))
		stats.ParseDuration += time.Since(parseStart)
		if err != nil {
			return stats, err
		}

		if err := m.recordUpload(ctx, pg, &file); err != nil {
			return stats, err
		}

		m.setJobStage(queued.ID, "chunking")
		chunkStart := time.Now()
		chunks := doc.Chunks
		if len(chunks) == 0 {
			chunks = m.chunker.ChunkDocument(doc)
		}
		stats.ChunkDuration += time.Since(chunkStart)
		stats.TotalChunks += len(chunks)
		log.Printf("[submit-phase] job=%s file=%s chunked_chunks=%d total_chunks=%d queue_cap=%d", queued.ID, file.OriginalName, len(chunks), stats.TotalChunks, cap(chunkQueue))

		m.updateFile(queued.ID, file.FileID, func(r *model.FileResult) { r.Status = "chunked" })
		m.updateJob(queued.ID, func(j *model.UploadJob) {
			j.TotalChunks += len(chunks)
			j.CurrentFile = file.OriginalName
			j.CurrentKind = file.DetectedKind
			j.ProgressLabel = fmt.Sprintf("Chunked %d of %d files into %d chunks", i+1, len(queued.Files), j.TotalChunks)
			j.ProgressPercent = clampPct(18 + ((i + 1) * 24 / maxInt(len(queued.Files), 1)))
			j.UpdatedAt = time.Now().UTC()
		})

		for _, chunk := range chunks {
			select {
			case <-ctx.Done():
				return stats, ctx.Err()
			case chunkQueue <- chunk:
				queuedChunks++
				if queuedChunks == 1 || queuedChunks%25 == 0 || queuedChunks == stats.TotalChunks {
					log.Printf("[submit-phase] job=%s queued_chunks=%d total_chunks=%d queue_depth=%d", queued.ID, queuedChunks, stats.TotalChunks, len(chunkQueue))
				}
			}
		}
	}

	return stats, nil
}

func (m *Manager) extractDocument(ctx context.Context, jobID string, file model.StagedFile, idx, total int) (model.ParsedDocument, error) {
	stop := func() {}
	if file.DetectedKind == "video" {
		stop = m.startVideoProgress(jobID, file, idx, total)
	} else {
		m.setJobStageDetailed(jobID, "extracting", file, idx, total, 18)
	}

	doc, err := m.router.Extract(ctx, file)
	stop()
	if err != nil {
		return model.ParsedDocument{}, fmt.Errorf("extract failed for %s: %w", file.OriginalName, err)
	}
	if err := validateDocumentLimits(doc); err != nil {
		return model.ParsedDocument{}, err
	}

	m.updateFile(jobID, file.FileID, func(r *model.FileResult) {
		r.Status = "parsed"
		r.Pages = len(doc.PageTexts)
	})
	m.updateJob(jobID, func(j *model.UploadJob) {
		j.CurrentFile = file.OriginalName
		j.CurrentKind = file.DetectedKind
		j.ProgressLabel = fmt.Sprintf("Extracted %d of %d files", idx+1, total)
		j.ProgressPercent = clampPct(10 + ((idx + 1) * 18 / maxInt(total, 1)))
		j.UpdatedAt = time.Now().UTC()
	})

	return doc, nil
}

func (m *Manager) recordUpload(ctx context.Context, pg *uploadrepo.Repository, file *model.StagedFile) error {
	if err := m.parser.AttachCloudURL(ctx, file); err != nil {
		return fmt.Errorf("cloud upload failed for %s: %w", file.OriginalName, err)
	}

	url := strings.TrimSpace(file.CloudURL)
	if url == "" {
		url = strings.TrimSpace(file.SourceURL)
	}
	if url == "" {
		return nil
	}

	if _, err := pg.CreateForChat(ctx, file.ChatID, url, file.DetectedKind, file.OriginalName); err != nil {
		return err
	}
	return nil
}
