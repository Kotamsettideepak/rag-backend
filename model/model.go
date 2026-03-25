package model

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
	CloudURL      string `json:"cloud_url,omitempty"`
	Size          int64  `json:"size"`
	ContentType   string `json:"content_type"`
	DetectedKind  string `json:"detected_kind"`
	OriginalOrder int    `json:"original_order"`
	ChatID        string `json:"chat_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`
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
	Detail          string          `json:"detail,omitempty"`
	CurrentFile     string          `json:"current_file,omitempty"`
	CurrentKind     string          `json:"current_kind,omitempty"`
	ProgressLabel   string          `json:"progress_label,omitempty"`
	ProgressPercent int             `json:"progress_percent,omitempty"`
	Error           string          `json:"error,omitempty"`
	Metrics         JobMetrics      `json:"metrics"`
}

type ParsedDocument struct {
	FileID      string
	FileName    string
	FileKind    string
	Text        string
	PageTexts   []string
	AudioChunks []AudioTranscriptChunk
	ChatID      string
	UserID      string
}

type Chunk struct {
	ID       string
	FileID   string
	FileName string
	FileKind string
	ChatID   string
	UserID   string
	Page     int
	Index    int
	Text     string
	Hash     string
	Metadata map[string]interface{}
}

type AudioTranscriptChunk struct {
	Content string
	Start   float64
	End     float64
	Type    string
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
