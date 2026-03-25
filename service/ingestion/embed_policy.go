package ingestion

import "gin-backend/model"

type embedBatchPolicy struct {
	MaxChunks int
	MaxChars  int
	MaxBytes  int
}

func loadEmbedBatchPolicy() embedBatchPolicy {
	return embedBatchPolicy{
		MaxChunks: envInt("INGEST_MAX_EMBED_CHUNKS_PER_REQUEST", 4),
		MaxChars:  envInt("INGEST_MAX_EMBED_CHARS_PER_REQUEST", 12000),
		MaxBytes:  envInt("INGEST_MAX_EMBED_BYTES_PER_REQUEST", 48000),
	}
}

func (p embedBatchPolicy) canAppend(batch []model.Chunk, chars, bytes int, chunk model.Chunk) bool {
	if len(batch) == 0 {
		return true
	}

	if p.MaxChunks > 0 && len(batch) >= p.MaxChunks {
		return false
	}

	nextChars := chars + len(chunk.Text)
	if p.MaxChars > 0 && nextChars > p.MaxChars {
		return false
	}

	nextBytes := bytes + len([]byte(chunk.Text))
	if p.MaxBytes > 0 && nextBytes > p.MaxBytes {
		return false
	}

	return true
}
