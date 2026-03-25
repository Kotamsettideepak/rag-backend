package ingestion

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"sync"
	"time"

	"gin-backend/model"
	"gin-backend/pkg/worker"
	"gin-backend/repository/vector"
	"gin-backend/service/ingestion/chunker"
	"gin-backend/service/ingestion/embedding"
	"gin-backend/service/ingestion/extractor"
	"gin-backend/service/ingestion/parser"
	retrievalsvc "gin-backend/service/retrieval"

	"github.com/google/uuid"
)

// Manager is the central ingestion orchestrator.
type Manager struct {
	parser         *parser.Parser
	router         *extractor.Router
	chunker        *chunker.Chunker
	embedder       *embedding.Service
	store          *vector.Repository
	pool           *worker.Pool
	jobQueue       chan queuedJob
	jobs           map[string]*model.UploadJob
	jobSubs        map[string]map[string]chan *model.UploadJob
	mu             sync.RWMutex
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	storeBatchSize int
	queryTopK      int
	chunkQueueSize int
	resultBufSize  int
	embedPolicy    embedBatchPolicy
}

type queuedJob struct {
	ID    string
	Files []model.StagedFile
}

// NewManager constructs a ready-to-use Manager.
func NewManager(svc *embedding.Service) *Manager {
	m := &Manager{
		parser:         parser.New(),
		router:         extractor.NewRouter(),
		chunker:        chunker.New(envInt("INGEST_CHUNK_SIZE", 3500), envInt("INGEST_CHUNK_OVERLAP", 700)),
		embedder:       svc,
		store:          vector.NewRepository(),
		jobs:           make(map[string]*model.UploadJob),
		jobSubs:        make(map[string]map[string]chan *model.UploadJob),
		storeBatchSize: envInt("STORE_BATCH_SIZE", 64),
		queryTopK:      envInt("QUERY_TOP_K", 10),
		embedPolicy:    loadEmbedBatchPolicy(),
	}

	workers := envInt("INGEST_WORKERS", 8)
	queueSz := envInt("INGEST_QUEUE_SIZE", workers*8)
	rateLimit := envInt("EMBED_RATE_LIMIT_PER_SECOND", 0)
	m.pool = worker.NewPool(svc, workers, queueSz, rateLimit)
	m.chunkQueueSize = envInt("INGEST_CHUNK_QUEUE_SIZE", workers*12)
	m.resultBufSize = envInt("INGEST_VECTOR_QUEUE_SIZE", workers*8)
	m.jobQueue = make(chan queuedJob, envInt("JOB_QUEUE_SIZE", 64))

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	workerCount := envInt("INGEST_JOB_WORKERS", 2)
	for i := 0; i < workerCount; i++ {
		m.wg.Add(1)
		go m.runJobWorker(ctx, i+1)
	}

	return m
}

// Shutdown gracefully stops all background workers.
func (m *Manager) Shutdown() {
	m.cancel()
	close(m.jobQueue)
	m.wg.Wait()
	m.pool.Shutdown()
}

// SubmitUpload stages files and enqueues an ingestion job.
func (m *Manager) SubmitUpload(files []*multipart.FileHeader, chatID, userID string) (*model.UploadJob, error) {
	staged, err := m.parser.StageFiles(files, chatID, userID)
	if err != nil {
		return nil, err
	}

	jobID := uuid.NewString()
	now := time.Now().UTC()
	job := &model.UploadJob{
		ID:              jobID,
		Status:          model.JobQueued,
		Stage:           "queued",
		CreatedAt:       now,
		UpdatedAt:       now,
		QueuedAt:        now,
		FileCount:       len(staged),
		Files:           make([]model.FileResult, 0, len(staged)),
		Summary:         stageLabel("queued"),
		Detail:          "Your upload was accepted and is waiting for a worker to begin processing.",
		ProgressLabel:   fmt.Sprintf("0 of %d files started", len(staged)),
		ProgressPercent: 2,
	}
	for _, f := range staged {
		job.Files = append(job.Files, model.FileResult{FileID: f.FileID, FileName: f.OriginalName, Status: "queued"})
	}

	m.mu.Lock()
	m.jobs[jobID] = job
	m.mu.Unlock()

	log.Printf("[jobs] queued job=%s chat=%s files=%d", jobID, chatID, len(staged))
	m.jobQueue <- queuedJob{ID: jobID, Files: staged}
	return cloneJob(job), nil
}

// GetJob returns a snapshot of the job or (nil, false).
func (m *Manager) GetJob(jobID string) (*model.UploadJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}
	return cloneJob(job), true
}

// SubscribeJob returns a channel that receives job snapshots on every state change.
func (m *Manager) SubscribeJob(jobID string) (<-chan *model.UploadJob, func(), error) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return nil, nil, fmt.Errorf("job not found")
	}

	subID := uuid.NewString()
	updates := make(chan *model.UploadJob, 8)
	if _, exists := m.jobSubs[jobID]; !exists {
		m.jobSubs[jobID] = make(map[string]chan *model.UploadJob)
	}
	m.jobSubs[jobID][subID] = updates
	snapshot := cloneJob(job)
	m.mu.Unlock()

	updates <- snapshot

	unsub := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		subs, exists := m.jobSubs[jobID]
		if !exists {
			return
		}
		ch, exists := subs[subID]
		if !exists {
			return
		}
		delete(subs, subID)
		close(ch)
		if len(subs) == 0 {
			delete(m.jobSubs, jobID)
		}
	}

	return updates, unsub, nil
}

// ClearContext wipes the entire Chroma collection.
func (m *Manager) ClearContext() error {
	return m.store.ClearCollection()
}

// DeleteChatContext removes all vectors for a chat.
func (m *Manager) DeleteChatContext(chatID, userID string) error {
	return m.store.DeleteByMetadata(map[string]interface{}{"chat_id": chatID, "user_id": userID})
}

// SearchContext embeds question and retrieves relevant chunks.
func (m *Manager) SearchContext(ctx context.Context, question, chatID, userID string) (model.SearchContextResult, error) {
	embedding, err := m.embedder.EmbedQuery(ctx, question)
	if err != nil {
		return model.SearchContextResult{}, err
	}

	_, topK := resolveTopK(question, m.queryTopK)
	matches, err := m.store.Search(embedding, topK, map[string]interface{}{"chat_id": chatID, "user_id": userID})
	if err != nil {
		return model.SearchContextResult{}, err
	}

	return retrievalsvc.BuildContextResult(question, matches, m.store), nil
}

func (m *Manager) VectorStore() *vector.Repository {
	return m.store
}
