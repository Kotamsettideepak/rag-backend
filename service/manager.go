package service

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"os"
	"strconv"
	"sync"
	"time"

	"gin-backend/db"
	"gin-backend/embedding"
	"gin-backend/extractor"
	"gin-backend/models"
	"gin-backend/worker"
)

type Manager struct {
	parser    *Parser
	extractor extractor.Client
	chunker   *Chunker
	embedder  *embedding.Service
	store     *db.ChromaStore
	pool      *worker.Pool
	jobQueue  chan queuedJob
	jobs      map[string]*models.UploadJob
	jobSubs   map[string]map[string]chan *models.UploadJob
	mu        sync.RWMutex
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	batchSize int
	queryTopK int
}

type queuedJob struct {
	ID    string
	Files []models.StagedFile
}

func NewManager() *Manager {
	ollamaClient := models.NewOllamaClient()
	cache := embedding.NewCache()
	embedder := embedding.NewService(ollamaClient, cache)

	manager := &Manager{
		parser:    NewParser(),
		extractor: extractor.NewHTTPClient(),
		chunker:   NewChunker(getEnvInt("INGEST_CHUNK_SIZE", 3500), getEnvInt("INGEST_CHUNK_OVERLAP", 700)),
		embedder:  embedder,
		store:     db.NewChromaStore(),
		jobs:      make(map[string]*models.UploadJob),
		jobSubs:   make(map[string]map[string]chan *models.UploadJob),
		batchSize: getEnvInt("INGEST_BATCH_SIZE", 8),
		queryTopK: getEnvInt("QUERY_TOP_K", 4),
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

func (m *Manager) SubmitUpload(files []*multipart.FileHeader) (*models.UploadJob, error) {
	stagedFiles, err := m.parser.StageFiles(files)
	if err != nil {
		return nil, err
	}

	jobID := generateID()
	now := time.Now().UTC()

	job := &models.UploadJob{
		ID:        jobID,
		Status:    models.JobQueued,
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

func (m *Manager) SearchContext(ctx context.Context, question string) (string, error) {
	embeddingVector, err := m.embedder.EmbedQuery(ctx, question)
	if err != nil {
		return "", err
	}

	return m.store.Search(embeddingVector, m.queryTopK)
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
			log.Printf("[ingest] job worker=%d picked job=%s file_count=%d", workerID, queued.ID, len(queued.Files))
			m.processJob(ctx, queued)
		}
	}
}

func (m *Manager) processJob(parentCtx context.Context, queued queuedJob) {
	if _, ok := m.GetJob(queued.ID); !ok {
		return
	}

	startedAt := time.Now().UTC()
	m.updateJob(queued.ID, func(target *models.UploadJob) {
		target.Status = models.JobProcessing
		target.UpdatedAt = startedAt
		target.StartedAt = &startedAt
	})

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	defer m.parser.Cleanup(queued.Files)

	processStarted := time.Now()
	parseStart := time.Now()
	documents := make([]models.ParsedDocument, 0, len(queued.Files))

	for _, file := range queued.Files {
		document, err := m.extractor.Extract(ctx, file)
		if err != nil {
			m.failJob(queued.ID, fmt.Errorf("extract failed for %s: %w", file.OriginalName, err))
			return
		}

		documents = append(documents, document)
		m.updateFile(queued.ID, file.FileID, func(result *models.FileResult) {
			result.Status = "parsed"
			result.Pages = len(document.PageTexts)
		})
	}
	parseDuration := time.Since(parseStart)

	chunkStart := time.Now()
	allChunks := make([]models.Chunk, 0)
	summaries := make([]string, 0, len(documents))
	for _, document := range documents {
		chunks := m.chunker.ChunkDocument(document)
		allChunks = append(allChunks, chunks...)
		summaries = append(summaries, document.Text)

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
			target.CompletedAt = &completedAt
			target.UpdatedAt = completedAt
			target.Summary = "Upload completed, but no extractable text was found."
			target.Metrics.TotalDurationMs = time.Since(processStarted).Milliseconds()
		})
		return
	}

	batches := splitIntoBatches(allChunks, m.batchSize)
	resultChannel := make(chan worker.BatchResult, len(batches))
	for _, batch := range batches {
		m.pool.Submit(worker.BatchTask{
			Ctx:      ctx,
			JobID:    queued.ID,
			Batch:    batch,
			Response: resultChannel,
		})
	}

	var embeddingDuration time.Duration
	var storageDuration time.Duration
	for range batches {
		result := <-resultChannel
		embeddingDuration += result.Duration

		if result.Err != nil {
			m.failJob(queued.ID, result.Err)
			return
		}

		storeStart := time.Now()
		if err := m.store.AddRecords(result.Records); err != nil {
			m.failJob(queued.ID, fmt.Errorf("vector store failed: %w", err))
			return
		}
		storageDuration += time.Since(storeStart)

		m.updateJob(queued.ID, func(target *models.UploadJob) {
			target.CompletedChunks += result.Processed
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

	summary := GenerateUploadSummary(summaries)
	completedAt := time.Now().UTC()
	m.updateJob(queued.ID, func(target *models.UploadJob) {
		target.Status = models.JobCompleted
		target.CompletedAt = &completedAt
		target.UpdatedAt = completedAt
		target.Summary = summary
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

	log.Printf(
		"[ingest] job=%s completed files=%d chunks=%d total=%s parse=%s chunk=%s embed=%s store=%s throughput=%.2f chunks/sec",
		queued.ID,
		len(queued.Files),
		len(allChunks),
		totalDuration,
		parseDuration,
		chunkDuration,
		embeddingDuration,
		storageDuration,
		throughput,
	)
}

func (m *Manager) failJob(jobID string, err error) {
	log.Printf("[ingest] job=%s failed: %v", jobID, err)
	completedAt := time.Now().UTC()
	m.updateJob(jobID, func(target *models.UploadJob) {
		target.Status = models.JobFailed
		target.Error = err.Error()
		target.CompletedAt = &completedAt
		target.UpdatedAt = completedAt
		target.Metrics.TotalDurationMs = target.UpdatedAt.Sub(target.CreatedAt).Milliseconds()
	})
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
