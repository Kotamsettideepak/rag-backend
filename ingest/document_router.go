package ingest

import (
	"context"
	"fmt"

	"gin-backend/audio"
	"gin-backend/extractor"
	"gin-backend/models"
	"gin-backend/visual"
	"gin-backend/youtube"
)

type documentExtractor interface {
	Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error)
}

type DocumentRouter struct {
	extractors map[string]documentExtractor
}

func NewDocumentRouter() *DocumentRouter {
	return &DocumentRouter{
		extractors: map[string]documentExtractor{
			KindPDF:     extractor.NewHTTPClient(),
			KindAudio:   audio.NewHTTPClient(),
			KindImage:   visual.NewHTTPClient(),
			KindYouTube: youtube.NewHTTPClient(),
		},
	}
}

func (r *DocumentRouter) Extract(ctx context.Context, file models.StagedFile) (models.ParsedDocument, error) {
	extractor, ok := r.extractors[file.DetectedKind]
	if !ok {
		return models.ParsedDocument{}, fmt.Errorf("unsupported file kind: %s", file.DetectedKind)
	}

	return extractor.Extract(ctx, file)
}
