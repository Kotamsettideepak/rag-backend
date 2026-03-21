package models

import "time"

type SearchMatch struct {
	ID       string
	Document string
	Metadata map[string]interface{}
}

type SearchContextResult struct {
	Context  string
	Modality string
}

type UploadJobStatus string

const (
	JobQueued     UploadJobStatus = "queued"
	JobProcessing UploadJobStatus = "processing"
	JobCompleted  UploadJobStatus = "completed"
	JobFailed     UploadJobStatus = "failed"
)

type StagedFile struct {
	FileID        string `json:"file_id"`
	OriginalName  string `json:"original_name"`
	StoredPath    string `json:"stored_path"`
	SourceURL     string `json:"source_url,omitempty"`
	Size          int64  `json:"size"`
	ContentType   string `json:"content_type"`
	DetectedKind  string `json:"detected_kind"`
	OriginalOrder int    `json:"original_order"`
}

type FileResult struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	Pages    int    `json:"pages"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

type JobMetrics struct {
	ParseDurationMs     int64   `json:"parse_duration_ms"`
	ChunkDurationMs     int64   `json:"chunk_duration_ms"`
	EmbeddingDurationMs int64   `json:"embedding_duration_ms"`
	StorageDurationMs   int64   `json:"storage_duration_ms"`
	TotalDurationMs     int64   `json:"total_duration_ms"`
	ThroughputChunksSec float64 `json:"throughput_chunks_sec"`
}

type UploadJob struct {
	ID              string          `json:"job_id"`
	Status          UploadJobStatus `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	QueuedAt        time.Time       `json:"queued_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	FileCount       int             `json:"file_count"`
	Files           []FileResult    `json:"files"`
	TotalChunks     int             `json:"total_chunks"`
	CompletedChunks int             `json:"completed_chunks"`
	FailedChunks    int             `json:"failed_chunks"`
	Stage           string          `json:"stage,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	Error           string          `json:"error,omitempty"`
	Metrics         JobMetrics      `json:"metrics"`
}

type ParsedDocument struct {
	FileID    string
	FileName  string
	FileKind  string
	Text      string
	PageTexts []string
}

type Chunk struct {
	ID       string
	FileID   string
	FileName string
	FileKind string
	Page     int
	Index    int
	Text     string
	Hash     string
}

type ChunkResult struct {
	Chunk         Chunk
	FromCache     bool
	EmbeddingDims int
	Error         error
}

type VectorRecord struct {
	ID        string
	Text      string
	Embedding []float64
	Metadata  map[string]interface{}
}
