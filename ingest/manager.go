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

func NewManager() *Manager {
	ollamaClient := models.NewOllamaClient()
	embedder := embedding.NewService(ollamaClient)

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
		ID:        jobID,
		Status:    models.JobQueued,
		Stage:     "queued",
		CreatedAt: now,
		UpdatedAt: now,
		QueuedAt:  now,
		FileCount: len(stagedFiles),
		Files:     make([]models.FileResult, 0, len(stagedFiles)),
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
		ID:        jobID,
		Status:    models.JobQueued,
		Stage:     "queued",
		CreatedAt: now,
		UpdatedAt: now,
		QueuedAt:  now,
		FileCount: len(stagedFiles),
		Files:     make([]models.FileResult, 0, len(stagedFiles)),
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

	matches, err := m.store.Search(embeddingVector, m.queryTopK, map[string]interface{}{
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

	for _, file := range queued.Files {
		stopProgress := func() {}
		if file.DetectedKind == KindYouTube {
			stopProgress = m.startYouTubeExtractionProgress(queued.ID)
		} else {
			m.setJobStage(queued.ID, "extracting")
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
	}
	parseDuration := time.Since(parseStart)

	chunkStart := time.Now()
	allChunks := make([]models.Chunk, 0)
	m.setJobStage(queued.ID, "chunking")
	trace.Mark("INGEST", "starting chunking")
	for _, document := range documents {
		chunks := m.chunker.ChunkDocument(document)
		allChunks = append(allChunks, chunks...)
		log.Printf("[ingest] chunked file=%s kind=%s chunks=%d", document.FileName, document.FileKind, len(chunks))

		m.updateFile(queued.ID, document.FileID, func(result *models.FileResult) {
			result.Status = "chunked"
		})
	}
	chunkDuration := time.Since(chunkStart)

	m.updateJob(queued.ID, func(target *models.UploadJob) {
		target.TotalChunks = len(allChunks)
		target.Metrics.ParseDurationMs = parseDuration.Milliseconds()
		target.Metrics.ChunkDurationMs = chunkDuration.Milliseconds()
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
		target.CompletedAt = &completedAt
		target.UpdatedAt = completedAt
		target.Metrics.TotalDurationMs = target.UpdatedAt.Sub(target.CreatedAt).Milliseconds()
	})
}

func (m *Manager) setJobStage(jobID string, stage string) {
	m.updateJob(jobID, func(target *models.UploadJob) {
		target.Stage = strings.TrimSpace(stage)
		target.Summary = summarizeStage(stage)
		target.UpdatedAt = time.Now().UTC()
	})
}

func (m *Manager) startYouTubeExtractionProgress(jobID string) func() {
	m.setJobStage(jobID, "downloading")

	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()

		select {
		case <-timer.C:
			m.setJobStage(jobID, "transcribing")
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
