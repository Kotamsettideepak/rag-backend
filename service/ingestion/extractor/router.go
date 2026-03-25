package extractor

import (
	"context"
	"fmt"

	clientextractor "gin-backend/client/extractor"
	"gin-backend/client/gemini"
	grqaudio "gin-backend/client/groq"
	"gin-backend/client/video"
	"gin-backend/model"
)

// documentExtractor is the common interface for all file extractors.
type documentExtractor interface {
	Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error)
}

// Router dispatches to the correct extractor based on file kind.
type Router struct {
	extractors map[string]documentExtractor
}

// NewRouter wires all clients into the dispatch map.
func NewRouter() *Router {
	return &Router{
		extractors: map[string]documentExtractor{
			"pdf":   clientextractor.NewClient(),
			"audio": grqaudio.NewAudioClient(),
			"image": gemini.NewClient(),
			"video": video.NewClient(),
		},
	}
}

// Extract delegates to the appropriate extractor.
func (r *Router) Extract(ctx context.Context, file model.StagedFile) (model.ParsedDocument, error) {
	ext, ok := r.extractors[file.DetectedKind]
	if !ok {
		return model.ParsedDocument{}, fmt.Errorf("unsupported file kind: %s", file.DetectedKind)
	}
	return ext.Extract(ctx, file)
}
