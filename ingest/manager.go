package ingest

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gin-backend/db"
	"gin-backend/embedding"
	"gin-backend/models"
	pgstore "gin-backend/store"
	"gin-backend/trace"
	"gin-backend/worker"
)

type Manager struct {
	parser         *Parser
	router         *DocumentRouter
	chunker        *Chunker
	embedder       *embedding.Service
	store          *db.ChromaStore
	pool           *worker.Pool
	jobQueue       chan queuedJob
	jobs           map[string]*models.UploadJob
	jobSubs        map[string]map[string]chan *models.UploadJob
	mu             sync.RWMutex
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	batchSize      int
	storeBatchSize int
	queryTopK      int
}

type queuedJob struct {
	ID    string
	Files []models.StagedFile
}

func NewManager(embedder *embedding.Service) *Manager {
	manager := &Manager{
		parser:         NewParser(),
		router:         NewDocumentRouter(),
		chunker:        NewChunker(getEnvInt("INGEST_CHUNK_SIZE", 3500), getEnvInt("INGEST_CHUNK_OVERLAP", 700)),
		embedder:       embedder,
		store:          db.NewChromaStore(),
		jobs:           make(map[string]*models.UploadJob),
		jobSubs:        make(map[string]map[string]chan *models.UploadJob),
		batchSize:      getEnvInt("INGEST_BATCH_SIZE", 8),
		storeBatchSize: getEnvInt("STORE_BATCH_SIZE", 64),
		queryTopK:      getEnvInt("QUERY_TOP_K", 10),
	}

	workerCount := getEnvInt("INGEST_WORKERS", 8)
	queueSize := getEnvInt("INGEST_QUEUE_SIZE", workerCount*8)
	rateLimit := getEnvInt("EMBED_RATE_LIMIT_PER_SECOND", 0)
	manager.pool = worker.NewPool(embedder, workerCount, queueSize, rateLimit)
	manager.jobQueue = make(chan queuedJob, getEnvInt("JOB_QUEUE_SIZE", 64))

	ctx, cancel := context.WithCancel(context.Background())
	manager.cancel = cancel

	jobWorkers := getEnvInt("INGEST_JOB_WORKERS", 2)
	for index := 0; index < jobWorkers; index++ {
		manager.wg.Add(1)
		go manager.runJobWorker(ctx, index+1)
	}

	return manager
}

func (m *Manager) Shutdown() {
	m.cancel()
	close(m.jobQueue)
	m.wg.Wait()
	m.pool.Shutdown()
}

func (m *Manager) SubmitUpload(files []*multipart.FileHeader, chatID string, userID string) (*models.UploadJob, error) {
	stagedFiles, err := m.parser.StageFiles(files, chatID, userID)
	if err != nil {
		return nil, err
	}

	if err := recordUploads(context.Background(), stagedFiles); err != nil {
		return nil, err
	}

	jobID := generateID()
	now := time.Now().UTC()

	job := &models.UploadJob{
		ID:              jobID,
		Status:          models.JobQueued,
		Stage:           "queued",
		CreatedAt:       now,
		UpdatedAt:       now,
		QueuedAt:        now,
		FileCount:       len(stagedFiles),
		Files:           make([]models.FileResult, 0, len(stagedFiles)),
		Summary:         summarizeStage("queued"),
		Detail:          "Your upload was accepted and is waiting for a worker to begin processing.",
		ProgressLabel:   fmt.Sprintf("0 of %d files started", len(stagedFiles)),
		ProgressPercent: 2,
	}

	for _, file := range stagedFiles {
		job.Files = append(job.Files, models.FileResult{
			FileID:   file.FileID,
			FileName: file.OriginalName,
			Status:   "queued",
		})
	}

	m.mu.Lock()
	m.jobs[jobID] = job
	m.mu.Unlock()

	m.jobQueue <- queuedJob{ID: jobID, Files: stagedFiles}
	return cloneJob(job), nil
}

func (m *Manager) SubmitYouTube(url string, chatID string, userID string) (*models.UploadJob, error) {
	stagedFiles, err := m.parser.StageYouTubeURL(url, chatID, userID)
	if err != nil {
		return nil, err
	}

	if err := recordUploads(context.Background(), stagedFiles); err != nil {
		return nil, err
	}

	jobID := generateID()
	now := time.Now().UTC()
	job := &models.UploadJob{
		ID:              jobID,
		Status:          models.JobQueued,
		Stage:           "queued",
		CreatedAt:       now,
		UpdatedAt:       now,
		QueuedAt:        now,
		FileCount:       len(stagedFiles),
		Files:           make([]models.FileResult, 0, len(stagedFiles)),
		Summary:         summarizeStage("queued"),
		Detail:          "Your YouTube link was accepted and is waiting for background processing to begin.",
		ProgressLabel:   fmt.Sprintf("0 of %d items started", len(stagedFiles)),
		ProgressPercent: 2,
	}

	for _, file := range stagedFiles {
		job.Files = append(job.Files, models.FileResult{
			FileID:   file.FileID,
			FileName: file.OriginalName,
			Status:   "queued",
		})
	}

	m.mu.Lock()
	m.jobs[jobID] = job
	m.mu.Unlock()

	m.jobQueue <- queuedJob{ID: jobID, Files: stagedFiles}
	return cloneJob(job), nil
}

func (m *Manager) GetJob(jobID string) (*models.UploadJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}

	return cloneJob(job), true
}

func (m *Manager) SubscribeJob(jobID string) (<-chan *models.UploadJob, func(), error) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return nil, nil, fmt.Errorf("job not found")
	}

	subID := generateID()
	updates := make(chan *models.UploadJob, 8)
	if _, exists := m.jobSubs[jobID]; !exists {
		m.jobSubs[jobID] = make(map[string]chan *models.UploadJob)
	}
	m.jobSubs[jobID][subID] = updates
	snapshot := cloneJob(job)
	m.mu.Unlock()

	updates <- snapshot

	unsubscribe := func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		subscribers, exists := m.jobSubs[jobID]
		if !exists {
			return
		}

		ch, exists := subscribers[subID]
		if !exists {
			return
		}

		delete(subscribers, subID)
		close(ch)
		if len(subscribers) == 0 {
			delete(m.jobSubs, jobID)
		}
	}

	return updates, unsubscribe, nil
}

func (m *Manager) SearchContext(ctx context.Context, question string, chatID string, userID string) (models.SearchContextResult, error) {
	log.Printf("[search] question=%s", previewText(question, 220))
	embeddingVector, err := m.embedder.EmbedQuery(ctx, question)
	if err != nil {
		return models.SearchContextResult{}, err
	}
	log.Printf("[search] question embedding dims=%d", len(embeddingVector))

	complexity, topK := m.resolveQueryTopK(question)
	log.Printf("[search] complexity=%s top_k=%d", complexity, topK)

	matches, err := m.store.Search(embeddingVector, topK, map[string]interface{}{
		"chat_id": chatID,
		"user_id": userID,
	})
	if err != nil {
		return models.SearchContextResult{}, err
	}
	result := buildSearchContextResult(question, matches, m.store)
	log.Printf(
		"[search] context modality=%s matches=%d context_chars=%d preview=%s",
		result.Modality,
		len(matches),
		len(result.Context),
		previewText(result.Context, 320),
	)
	return result, nil
}

func (m *Manager) resolveQueryTopK(question string) (string, int) {
	normalized := strings.ToLower(strings.TrimSpace(question))
	if normalized == "" {
		return "simple", clampTopK(4, m.queryTopK)
	}

	wordCount := len(strings.Fields(normalized))
	complexityScore := 0

	if wordCount >= 7 {
		complexityScore++
	}
	if wordCount >= 14 {
		complexityScore++
	}

	complexitySignals := []string{
		"why", "how", "explain", "compare", "difference", "summarize", "summary",
		"relationship", "relationships", "analyze", "analysis", "describe", "details",
		"step by step", "based on", "evidence", "context", "overall", "multiple",
	}
	for _, signal := range complexitySignals {
		if strings.Contains(normalized, signal) {
			complexityScore++
		}
	}

	switch {
	case complexityScore >= 3:
		return "complex", clampTopK(10, m.queryTopK)
	case complexityScore >= 1:
		return "medium", clampTopK(7, m.queryTopK)
	default:
		return "simple", clampTopK(4, m.queryTopK)
	}
}

func clampTopK(desired int, fallback int) int {
	if desired <= 0 {
		desired = 1
	}
	if fallback <= 0 {
		fallback = desired
	}
	if desired > fallback {
		return fallback
	}
	return desired
}

func recordUploads(ctx context.Context, stagedFiles []models.StagedFile) error {
	pg := pgstore.DefaultStore()
	if pg == nil {
		return fmt.Errorf("database store is not initialized")
	}

	for _, file := range stagedFiles {
		fileURL := strings.TrimSpace(file.CloudURL)
		if fileURL == "" {
			fileURL = strings.TrimSpace(file.SourceURL)
		}
		if fileURL == "" {
			continue
		}

		if _, err := pg.CreateUserUploadedData(ctx, file.ChatID, fileURL, file.DetectedKind, file.OriginalName); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) ClearContext() error {
	return m.store.ClearCollection()
}

func (m *Manager) DeleteChatContext(chatID string, userID string) error {
	return m.store.DeleteByMetadata(map[string]interface{}{
		"chat_id": chatID,
		"user_id": userID,
	})
}

func (m *Manager) runJobWorker(ctx context.Context, workerID int) {
	defer m.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case queued, ok := <-m.jobQueue:
			if !ok {
				return
			}
			m.processJob(ctx, queued)
		}
	}
}

func (m *Manager) processJob(parentCtx context.Context, queued queuedJob) {
	if _, ok := m.GetJob(queued.ID); !ok {
		return
	}
	trace.Start("INGEST", "job_id="+queued.ID)

	startedAt := time.Now().UTC()
	m.updateJob(queued.ID, func(target *models.UploadJob) {
		target.Status = models.JobProcessing
		target.Stage = "processing"
		target.Summary = summarizeStage("processing")
		target.Detail = "We are validating the request, preparing temporary files, and selecting the correct extraction pipeline."
		target.ProgressLabel = fmt.Sprintf("0 of %d files prepared", len(queued.Files))
		target.ProgressPercent = 5
		target.UpdatedAt = startedAt
		target.StartedAt = &startedAt
	})

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	defer m.parser.Cleanup(queued.Files)

	processStarted := time.Now()
	parseStart := time.Now()
	documents := make([]models.ParsedDocument, 0, len(queued.Files))
	trace.Mark("INGEST", "starting extraction")

	for index, file := range queued.Files {
		stopProgress := func() {}
		if file.DetectedKind == KindYouTube {
			stopProgress = m.startYouTubeExtractionProgress(queued.ID, file, index, len(queued.Files))
		} else {
			m.setJobStageDetailed(queued.ID, "extracting", file, index, len(queued.Files), 18)
		}

		document, err := m.router.Extract(ctx, file)
		stopProgress()
		if err != nil {
			trace.End("INGEST", "extract failed file="+file.OriginalName)
			m.failJob(queued.ID, fmt.Errorf("extract failed for %s: %w", file.OriginalName, err))
			return
		}

		documents = append(documents, document)
		log.Printf(
			"[ingest] extracted file=%s kind=%s pages=%d text_chars=%d preview=%s",
			document.FileName,
			document.FileKind,
			len(document.PageTexts),
			len(document.Text),
			previewText(document.Text, 220),
		)
		m.updateFile(queued.ID, file.FileID, func(result *models.FileResult) {
			result.Status = "parsed"
			result.Pages = len(document.PageTexts)
		})
		m.updateJob(queued.ID, func(target *models.UploadJob) {
			target.CurrentFile = file.OriginalName
			target.CurrentKind = file.DetectedKind
			target.Detail = fmt.Sprintf("Finished extracting usable content from %s.", file.OriginalName)
			target.ProgressLabel = fmt.Sprintf("Extracted %d of %d files", index+1, len(queued.Files))
			target.ProgressPercent = clampPercent(10 + ((index + 1) * 25 / maxInt(len(queued.Files), 1)))
			target.UpdatedAt = time.Now().UTC()
		})
	}
	parseDuration := time.Since(parseStart)

	chunkStart := time.Now()
	allChunks := make([]models.Chunk, 0)
	m.setJobStage(queued.ID, "chunking")
	trace.Mark("INGEST", "starting chunking")
	for index, document := range documents {
		chunks := m.chunker.ChunkDocument(document)
		allChunks = append(allChunks, chunks...)
		log.Printf("[ingest] chunked file=%s kind=%s chunks=%d", document.FileName, document.FileKind, len(chunks))

		m.updateFile(queued.ID, document.FileID, func(result *models.FileResult) {
			result.Status = "chunked"
		})
		m.updateJob(queued.ID, func(target *models.UploadJob) {
			target.CurrentFile = document.FileName
			target.CurrentKind = document.FileKind
			target.Detail = fmt.Sprintf("Organizing extracted content from %s into retrieval chunks.", document.FileName)
			target.ProgressLabel = fmt.Sprintf("Chunked %d of %d files into %d chunks", index+1, len(documents), len(allChunks))
			target.ProgressPercent = clampPercent(40 + ((index + 1) * 15 / maxInt(len(documents), 1)))
			target.UpdatedAt = time.Now().UTC()
		})
	}
	chunkDuration := time.Since(chunkStart)

	m.updateJob(queued.ID, func(target *models.UploadJob) {
		target.TotalChunks = len(allChunks)
		target.Metrics.ParseDurationMs = parseDuration.Milliseconds()
		target.Metrics.ChunkDurationMs = chunkDuration.Milliseconds()
		target.Detail = fmt.Sprintf("Prepared %d retrieval chunks. Next we will generate embeddings.", len(allChunks))
		target.ProgressLabel = fmt.Sprintf("%d chunks ready for embeddings", len(allChunks))
		target.ProgressPercent = 55
		target.UpdatedAt = time.Now().UTC()
	})

	if len(allChunks) == 0 {
		completedAt := time.Now().UTC()
		m.updateJob(queued.ID, func(target *models.UploadJob) {
			target.Status = models.JobCompleted
			target.Stage = "completed"
			target.CompletedAt = &completedAt
			target.UpdatedAt = completedAt
			target.Summary = "Upload completed, but no extractable text was found."
			target.Detail = "The pipeline finished, but there was no usable text to index for chat."
			target.ProgressLabel = "Nothing was indexed"
			target.ProgressPercent = 100
			target.Metrics.TotalDurationMs = time.Since(processStarted).Milliseconds()
		})
		trace.End("INGEST", "completed with no extractable text")
		return
	}

	batches := splitIntoBatches(allChunks, m.batchSize)
	m.setJobStage(queued.ID, "embedding")
	trace.Mark("INGEST", fmt.Sprintf("starting embedding batches=%d total_chunks=%d", len(batches), len(allChunks)))
	resultChannel := make(chan worker.BatchResult, len(batches))
	for _, batch := range batches {
		if len(batch) > 0 {
			log.Printf(
				"[ingest] embedding batch job=%s batch_size=%d file=%s kind=%s first_chunk_idx=%d preview=%s",
				queued.ID,
				len(batch),
				batch[0].FileName,
				batch[0].FileKind,
				batch[0].Index,
				previewText(batch[0].Text, 180),
			)
		}
		m.pool.Submit(worker.BatchTask{
			Ctx:      ctx,
			JobID:    queued.ID,
			Batch:    batch,
			Response: resultChannel,
		})
	}

	var embeddingDuration time.Duration
	var storageDuration time.Duration
	pendingRecords := make([]models.VectorRecord, 0, minInt(len(allChunks), m.storeBatchSize))
	pendingProcessed := 0
	for range batches {
		result := <-resultChannel
		embeddingDuration += result.Duration

		if result.Err != nil {
			trace.End("INGEST", "embedding failed")
			m.failJob(queued.ID, result.Err)
			return
		}

		pendingRecords = append(pendingRecords, result.Records...)
		pendingProcessed += result.Processed

		if len(pendingRecords) >= m.storeBatchSize {
			m.setJobStage(queued.ID, "storing")
			log.Printf("[ingest] storing batch job=%s records=%d", queued.ID, len(pendingRecords))
			storeStart := time.Now()
			if err := m.store.AddRecords(pendingRecords); err != nil {
				trace.End("INGEST", "storage failed")
				m.failJob(queued.ID, fmt.Errorf("vector store failed: %w", err))
				return
			}
			storageDuration += time.Since(storeStart)

			m.updateJob(queued.ID, func(target *models.UploadJob) {
				target.CompletedChunks += pendingProcessed
				target.UpdatedAt = time.Now().UTC()
				target.Metrics.EmbeddingDurationMs = embeddingDuration.Milliseconds()
				target.Metrics.StorageDurationMs = storageDuration.Milliseconds()
				target.Detail = fmt.Sprintf("Generated embeddings for %d of %d chunks.", target.CompletedChunks, target.TotalChunks)
				target.ProgressLabel = fmt.Sprintf("Embedded %d of %d chunks", target.CompletedChunks, target.TotalChunks)
				target.ProgressPercent = clampPercent(55 + (target.CompletedChunks * 25 / maxInt(target.TotalChunks, 1)))
			})

			pendingRecords = pendingRecords[:0]
			pendingProcessed = 0
		}
	}

	if len(pendingRecords) > 0 {
		m.setJobStage(queued.ID, "storing")
		log.Printf("[ingest] storing final batch job=%s records=%d", queued.ID, len(pendingRecords))
		storeStart := time.Now()
		if err := m.store.AddRecords(pendingRecords); err != nil {
			trace.End("INGEST", "final storage failed")
			m.failJob(queued.ID, fmt.Errorf("vector store failed: %w", err))
			return
		}
		storageDuration += time.Since(storeStart)

		m.updateJob(queued.ID, func(target *models.UploadJob) {
			target.CompletedChunks += pendingProcessed
			target.UpdatedAt = time.Now().UTC()
			target.Metrics.EmbeddingDurationMs = embeddingDuration.Milliseconds()
			target.Metrics.StorageDurationMs = storageDuration.Milliseconds()
			target.Detail = fmt.Sprintf("Saved %d of %d chunks into the vector database.", target.CompletedChunks, target.TotalChunks)
			target.ProgressLabel = fmt.Sprintf("Stored %d of %d chunks", target.CompletedChunks, target.TotalChunks)
			target.ProgressPercent = clampPercent(82 + (target.CompletedChunks * 15 / maxInt(target.TotalChunks, 1)))
		})
	}

	totalDuration := time.Since(processStarted)
	throughput := float64(len(allChunks))
	if totalDuration > 0 {
		throughput = throughput / totalDuration.Seconds()
	}

	completedAt := time.Now().UTC()
	m.updateJob(queued.ID, func(target *models.UploadJob) {
		target.Status = models.JobCompleted
		target.Stage = "completed"
		target.CompletedAt = &completedAt
		target.UpdatedAt = completedAt
		target.Summary = "Upload completed. The files are ready for chat."
		target.Detail = fmt.Sprintf("Finished processing %d file(s). %d chunks are now indexed and searchable.", len(queued.Files), len(allChunks))
		target.ProgressLabel = "Ready for chat"
		target.ProgressPercent = 100
		target.Metrics.ParseDurationMs = parseDuration.Milliseconds()
		target.Metrics.ChunkDurationMs = chunkDuration.Milliseconds()
		target.Metrics.EmbeddingDurationMs = embeddingDuration.Milliseconds()
		target.Metrics.StorageDurationMs = storageDuration.Milliseconds()
		target.Metrics.TotalDurationMs = totalDuration.Milliseconds()
		target.Metrics.ThroughputChunksSec = throughput
		for index := range target.Files {
			if target.Files[index].Status != "failed" {
				target.Files[index].Status = "completed"
			}
		}
	})
	trace.End("INGEST", fmt.Sprintf("job_id=%s chunks=%d", queued.ID, len(allChunks)))

	log.Printf("[ingest] job=%s completed files=%d chunks=%d total=%s parse=%s chunk=%s embed=%s store=%s throughput=%.2f chunks/sec", queued.ID, len(queued.Files), len(allChunks), totalDuration, parseDuration, chunkDuration, embeddingDuration, storageDuration, throughput)
}

func (m *Manager) failJob(jobID string, err error) {
	log.Printf("[ingest] job=%s failed: %v", jobID, err)
	completedAt := time.Now().UTC()
	m.updateJob(jobID, func(target *models.UploadJob) {
		target.Status = models.JobFailed
		target.Stage = "failed"
		target.Error = err.Error()
		target.Summary = summarizeStage("failed")
		target.Detail = "The pipeline stopped before completion. Check the error message for the exact reason."
		target.ProgressLabel = "Processing stopped"
		target.ProgressPercent = 100
		target.CompletedAt = &completedAt
		target.UpdatedAt = completedAt
		target.Metrics.TotalDurationMs = target.UpdatedAt.Sub(target.CreatedAt).Milliseconds()
	})
}

func (m *Manager) setJobStage(jobID string, stage string) {
	m.updateJob(jobID, func(target *models.UploadJob) {
		target.Stage = strings.TrimSpace(stage)
		target.Summary = summarizeStage(stage)
		target.Detail = describeStage(stage, target.CurrentFile, target.CurrentKind)
		target.ProgressLabel = defaultProgressLabel(stage)
		target.ProgressPercent = defaultProgressPercent(stage)
		target.UpdatedAt = time.Now().UTC()
	})
}

func (m *Manager) setJobStageDetailed(jobID string, stage string, file models.StagedFile, fileIndex int, totalFiles int, fallbackPercent int) {
	m.updateJob(jobID, func(target *models.UploadJob) {
		target.Stage = strings.TrimSpace(stage)
		target.Summary = summarizeStage(stage)
		target.CurrentFile = strings.TrimSpace(file.OriginalName)
		target.CurrentKind = strings.TrimSpace(file.DetectedKind)
		target.Detail = describeStage(stage, target.CurrentFile, target.CurrentKind)
		target.ProgressLabel = fmt.Sprintf("%d of %d files in progress", fileIndex+1, maxInt(totalFiles, 1))
		target.ProgressPercent = fallbackPercent
		target.UpdatedAt = time.Now().UTC()
	})
}

func (m *Manager) startYouTubeExtractionProgress(jobID string, file models.StagedFile, fileIndex int, totalFiles int) func() {
	m.setJobStageDetailed(jobID, "downloading", file, fileIndex, totalFiles, 20)

	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()

		select {
		case <-timer.C:
			m.setJobStageDetailed(jobID, "transcribing", file, fileIndex, totalFiles, 32)
		case <-done:
			return
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
		})
	}
}

func (m *Manager) updateJob(jobID string, update func(*models.UploadJob)) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return
	}

	update(job)
	snapshot := cloneJob(job)
	subscribers := copySubscribers(m.jobSubs[jobID])
	m.mu.Unlock()
	m.publishJobSnapshot(snapshot, subscribers)
}

func (m *Manager) updateFile(jobID string, fileID string, update func(*models.FileResult)) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return
	}

	for index := range job.Files {
		if job.Files[index].FileID == fileID {
			update(&job.Files[index])
			job.UpdatedAt = time.Now().UTC()
			snapshot := cloneJob(job)
			subscribers := copySubscribers(m.jobSubs[jobID])
			m.mu.Unlock()
			m.publishJobSnapshot(snapshot, subscribers)
			return
		}
	}

	m.mu.Unlock()
}

func splitIntoBatches(chunks []models.Chunk, batchSize int) [][]models.Chunk {
	if batchSize <= 0 {
		batchSize = 1
	}

	batches := make([][]models.Chunk, 0, (len(chunks)/batchSize)+1)
	for start := 0; start < len(chunks); start += batchSize {
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batches = append(batches, chunks[start:end])
	}
	return batches
}

func cloneJob(job *models.UploadJob) *models.UploadJob {
	if job == nil {
		return nil
	}

	cloned := *job
	cloned.Files = append([]models.FileResult(nil), job.Files...)
	return &cloned
}

func copySubscribers(source map[string]chan *models.UploadJob) []chan *models.UploadJob {
	if len(source) == 0 {
		return nil
	}

	cloned := make([]chan *models.UploadJob, 0, len(source))
	for _, subscriber := range source {
		cloned = append(cloned, subscriber)
	}
	return cloned
}

func (m *Manager) publishJobSnapshot(job *models.UploadJob, subscribers []chan *models.UploadJob) {
	if job == nil || len(subscribers) == 0 {
		return
	}

	for _, subscriber := range subscribers {
		select {
		case subscriber <- cloneJob(job):
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- cloneJob(job):
			default:
			}
		}
	}
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

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func summarizeStage(stage string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "queued":
		return "Queued. Waiting to start processing."
	case "processing":
		return "Preparing your files for AI processing."
	case "extracting":
		return "Extracting data from your files."
	case "downloading":
		return "Downloading and preparing the video audio."
	case "transcribing":
		return "Transcribing audio into searchable text. This can take a little while for longer files."
	case "chunking":
		return "Normalizing and organizing the extracted content."
	case "embedding":
		return "Creating embeddings so your content can be searched semantically."
	case "storing":
		return "Saving everything to the vector database."
	case "completed":
		return "Your files are ready. You can start chatting now."
	case "failed":
		return "Processing failed."
	default:
		return "Processing your files."
	}
}

func describeStage(stage string, currentFile string, currentKind string) string {
	fileLabel := strings.TrimSpace(currentFile)
	if fileLabel == "" {
		fileLabel = "your content"
	}

	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "queued":
		return "Your request is waiting in the processing queue."
	case "processing":
		return "Preparing the upload, validating inputs, and choosing the right extraction path."
	case "extracting":
		return fmt.Sprintf("Extracting usable content from %s.", fileLabel)
	case "downloading":
		return fmt.Sprintf("Downloading the video and isolating the audio track from %s.", fileLabel)
	case "transcribing":
		return fmt.Sprintf("Converting the extracted audio from %s into searchable text.", fileLabel)
	case "chunking":
		return "Splitting the extracted content into retrieval-ready chunks."
	case "embedding":
		return "Generating vector embeddings so your content can be searched semantically."
	case "storing":
		return "Saving the processed chunks into the vector database."
	case "completed":
		return "Everything finished successfully and the content is ready for chat."
	case "failed":
		return "The job stopped before finishing."
	default:
		return "Processing your request."
	}
}

func defaultProgressLabel(stage string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "queued":
		return "Waiting to start"
	case "processing":
		return "Preparing job"
	case "extracting":
		return "Extracting content"
	case "downloading":
		return "Downloading video"
	case "transcribing":
		return "Transcribing audio"
	case "chunking":
		return "Building chunks"
	case "embedding":
		return "Generating embeddings"
	case "storing":
		return "Saving vectors"
	case "completed":
		return "Ready for chat"
	case "failed":
		return "Needs attention"
	default:
		return "Processing"
	}
}

func defaultProgressPercent(stage string) int {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "queued":
		return 2
	case "processing":
		return 5
	case "extracting":
		return 18
	case "downloading":
		return 20
	case "transcribing":
		return 32
	case "chunking":
		return 45
	case "embedding":
		return 60
	case "storing":
		return 85
	case "completed", "failed":
		return 100
	default:
		return 0
	}
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
