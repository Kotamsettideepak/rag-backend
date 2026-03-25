package groq

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gin-backend/config"
	"gin-backend/model"
)

const (
	minTranscriptChars    = 20
	minMergedChunkChars   = 50
	maxTranscriptionBytes = 25 * 1024 * 1024
)

type AudioClient interface {
	Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error)
}

type AudioHTTPClient struct {
	baseURL         string
	audioModels     []string
	apiKey          string
	client          *http.Client
	requestInterval time.Duration
	maxRetries      int
	rateMu          sync.Mutex
	nextRequestAt   time.Time
}

type transcriptionResponse struct {
	Text     string               `json:"text"`
	Duration float64              `json:"duration"`
	Segments []transcribedSegment `json:"segments"`
}

type transcribedSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type audioWindow struct {
	Start float64
	End   float64
}

func NewAudioClient() AudioClient {
	return &AudioHTTPClient{
		baseURL:         config.GetGroqBaseURL(),
		audioModels:     uniqueModels(config.GetGroqAudioModel(), config.GetGroqAudioFallbackModels()),
		apiKey:          config.GetGroqAPIKey(),
		client:          &http.Client{Timeout: config.GetGroqTimeout()},
		requestInterval: config.GetGroqAudioRequestInterval(),
		maxRetries:      config.GetGroqAudioMaxRetries(),
	}
}

func (c *AudioHTTPClient) Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return model.ParsedDocument{}, fmt.Errorf("GROQ_API_KEY is not configured")
	}

	fileData, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return model.ParsedDocument{}, err
	}

	if len(fileData) > 0 && len(fileData) <= maxTranscriptionBytes {
		response, err := c.transcribeFileData(ctx, fileData, staged.OriginalName, -1)
		if err == nil {
			duration := estimateAudioDuration(response)
			directChunks := buildChunksFromResponse(response)
			documentText, pageTexts := buildAudioTexts(staged, duration, directChunks)

			log.Printf(
				"[audio] extracted file=%s mode=direct duration=%.2fs segments=%d kept_chunks=%d text_chars=%d",
				staged.OriginalName,
				duration,
				len(response.Segments),
				len(directChunks),
				len(documentText),
			)

			return model.ParsedDocument{
				FileID:      staged.FileID,
				FileName:    staged.OriginalName,
				FileKind:    staged.DetectedKind,
				Text:        documentText,
				PageTexts:   pageTexts,
				AudioChunks: directChunks,
				ChatID:      staged.ChatID,
				UserID:      staged.UserID,
			}, nil
		}

		log.Printf("[audio] direct transcription failed; falling back to chunked mode file=%s err=%v", staged.OriginalName, err)
	} else {
		log.Printf("[audio] file exceeds direct transcription size limit; using chunked mode file=%s bytes=%d", staged.OriginalName, len(fileData))
	}

	duration, err := probeAudioDuration(ctx, staged.StoredPath)
	if err != nil {
		return model.ParsedDocument{}, err
	}

	windows := buildAudioWindows(duration)
	if len(windows) == 0 {
		return model.ParsedDocument{}, fmt.Errorf("audio duration is unavailable")
	}

	chunks, err := c.transcribeAudioChunks(ctx, staged, windows)
	if err != nil {
		return model.ParsedDocument{}, err
	}

	mergedChunks := mergeSmallTranscriptChunks(chunks)
	documentText, pageTexts := buildAudioTexts(staged, duration, mergedChunks)

	log.Printf(
		"[audio] extracted file=%s mode=chunked duration=%.2fs windows=%d kept_chunks=%d text_chars=%d",
		staged.OriginalName,
		duration,
		len(windows),
		len(mergedChunks),
		len(documentText),
	)

	return model.ParsedDocument{
		FileID:      staged.FileID,
		FileName:    staged.OriginalName,
		FileKind:    staged.DetectedKind,
		Text:        documentText,
		PageTexts:   pageTexts,
		AudioChunks: mergedChunks,
		ChatID:      staged.ChatID,
		UserID:      staged.UserID,
	}, nil
}

func (c *AudioHTTPClient) transcribeAudioChunks(
	ctx context.Context,
	staged model.StagedFile,
	windows []audioWindow,
) ([]model.AudioTranscriptChunk, error) {
	tempDir, err := os.MkdirTemp("", "audio-rag-chunks-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	chunks := make([]model.AudioTranscriptChunk, 0, len(windows))
	for index, window := range windows {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk-%03d.mp3", index))
		if err := splitAudioChunk(ctx, staged.StoredPath, chunkPath, window); err != nil {
			return nil, err
		}

		response, err := c.transcribeChunkFile(ctx, chunkPath, staged.OriginalName, index)
		if err != nil {
			return nil, err
		}

		chunk, ok := buildTranscriptChunk(window, response)
		if !ok {
			continue
		}

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}
