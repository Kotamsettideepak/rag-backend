package topicextractor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gin-backend/config"
	"gin-backend/model"
)

type Client interface {
	Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error)
	StreamExtract(ctx context.Context, staged model.StagedFile, onChunk func(model.Chunk) error) (int, int, error)
	StartTopicJob(ctx context.Context, jobID string, staged model.StagedFile) error
}

type HTTPClient struct {
	baseURL       string
	client        *http.Client
	pagesPerBatch int
	pageOverlap   int
	maxChars      int
	overlapParas  int
}

type extractResponse struct {
	Chunks []extractChunk `json:"chunks"`
}

type streamEvent struct {
	Type  string        `json:"type"`
	Chunk *extractChunk `json:"chunk,omitempty"`
	Error string        `json:"error,omitempty"`
}

type extractChunk struct {
	Text           string `json:"text"`
	SectionTitle   string `json:"section_title"`
	Type           string `json:"type"`
	ChunkType      string `json:"chunk_type"`
	CodeLanguage   string `json:"code_language"`
	FormulaLatex   string `json:"formula_latex"`
	HasFormula     bool   `json:"has_formula"`
	PictureCaption string `json:"picture_caption"`
	PictureClass   string `json:"picture_class"`
	PageNumber     int    `json:"page_number"`
	PageRange      string `json:"page_range"`
	PageFrom       int    `json:"page_from"`
	PageTo         int    `json:"page_to"`
}

func NewClient() Client {
	return &HTTPClient{
		baseURL:       config.GetTopicExtractorBaseURL(),
		client:        &http.Client{Timeout: 20 * time.Minute},
		pagesPerBatch: envInt("TOPIC_EXTRACTOR_PAGES_PER_BATCH", 5),
		pageOverlap:   envInt("TOPIC_EXTRACTOR_PAGE_OVERLAP", 1),
		maxChars:      envInt("TOPIC_EXTRACTOR_MAX_CHARS", 1200),
		overlapParas:  envInt("TOPIC_EXTRACTOR_OVERLAP_PARAGRAPHS", 1),
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error) {
	fileData, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return model.ParsedDocument{}, err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("title", strings.TrimSpace(staged.OriginalName)); err != nil {
		return model.ParsedDocument{}, err
	}
	if err := writer.WriteField("pages_per_batch", strconv.Itoa(c.pagesPerBatch)); err != nil {
		return model.ParsedDocument{}, err
	}
	if err := writer.WriteField("page_overlap", strconv.Itoa(c.pageOverlap)); err != nil {
		return model.ParsedDocument{}, err
	}
	if err := writer.WriteField("max_chars", strconv.Itoa(c.maxChars)); err != nil {
		return model.ParsedDocument{}, err
	}
	if err := writer.WriteField("overlap_paragraphs", strconv.Itoa(c.overlapParas)); err != nil {
		return model.ParsedDocument{}, err
	}

	part, err := writer.CreateFormFile("file", filepath.Base(staged.OriginalName))
	if err != nil {
		return model.ParsedDocument{}, err
	}
	if _, err := part.Write(fileData); err != nil {
		return model.ParsedDocument{}, err
	}
	if err := writer.Close(); err != nil {
		return model.ParsedDocument{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/extract/chunks", body)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return model.ParsedDocument{}, fmt.Errorf("topic extractor failed with status %d: %s", resp.StatusCode, string(raw))
	}

	var parsed extractResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return model.ParsedDocument{}, err
	}

	chunks := make([]model.Chunk, 0, len(parsed.Chunks))
	maxPage := 0
	for i, chunk := range parsed.Chunks {
		text := strings.TrimSpace(chunk.Text)
		if text == "" {
			continue
		}
		sum := sha256.Sum256([]byte(text))
		metadata := map[string]interface{}{
			"section_title":   strings.TrimSpace(chunk.SectionTitle),
			"page_range":      strings.TrimSpace(chunk.PageRange),
			"chunk_type":      strings.TrimSpace(firstNonEmpty(chunk.ChunkType, chunk.Type, "text")),
			"code_language":   strings.TrimSpace(chunk.CodeLanguage),
			"formula_latex":   strings.TrimSpace(chunk.FormulaLatex),
			"has_formula":     chunk.HasFormula,
			"picture_class":   strings.TrimSpace(chunk.PictureClass),
			"picture_caption": strings.TrimSpace(chunk.PictureCaption),
			"page_from":       chunk.PageFrom,
			"page_to":         chunk.PageTo,
			"chunk_idx":       i,
		}
		page := chunk.PageNumber
		if page <= 0 {
			page = 1
		}
		if page > maxPage {
			maxPage = page
		}
		chunks = append(chunks, model.Chunk{
			ID:       staged.FileID + "-" + shortHash(sum[:]) + "-" + strconv.Itoa(i),
			FileID:   staged.FileID,
			FileName: staged.OriginalName,
			FileKind: staged.DetectedKind,
			ChatID:   staged.ChatID,
			UserID:   staged.UserID,
			TopicID:  staged.TopicID,
			Page:     page,
			Index:    i,
			Text:     text,
			Hash:     hex.EncodeToString(sum[:]),
			Metadata: metadata,
		})
	}

	if len(chunks) == 0 {
		return model.ParsedDocument{}, fmt.Errorf("topic extractor returned no chunks for file %s", staged.OriginalName)
	}

	pageTexts := make([]string, maxPage)
	for page := range pageTexts {
		pageTexts[page] = fmt.Sprintf("Page %d", page+1)
	}

	return model.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  staged.DetectedKind,
		Text:      "",
		PageTexts: pageTexts,
		Chunks:    chunks,
		ChatID:    staged.ChatID,
		UserID:    staged.UserID,
		TopicID:   staged.TopicID,
	}, nil
}

func (c *HTTPClient) StreamExtract(ctx context.Context, staged model.StagedFile, onChunk func(model.Chunk) error) (int, int, error) {
	fileData, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return 0, 0, err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("title", strings.TrimSpace(staged.OriginalName)); err != nil {
		return 0, 0, err
	}
	if err := writer.WriteField("pages_per_batch", strconv.Itoa(c.pagesPerBatch)); err != nil {
		return 0, 0, err
	}
	if err := writer.WriteField("page_overlap", strconv.Itoa(c.pageOverlap)); err != nil {
		return 0, 0, err
	}
	if err := writer.WriteField("max_chars", strconv.Itoa(c.maxChars)); err != nil {
		return 0, 0, err
	}
	if err := writer.WriteField("overlap_paragraphs", strconv.Itoa(c.overlapParas)); err != nil {
		return 0, 0, err
	}
	part, err := writer.CreateFormFile("file", filepath.Base(staged.OriginalName))
	if err != nil {
		return 0, 0, err
	}
	if _, err := part.Write(fileData); err != nil {
		return 0, 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/extract/chunks/stream", body)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		doc, err := c.Extract(ctx, staged)
		if err != nil {
			return 0, 0, err
		}
		maxPage := 0
		for _, chunk := range doc.Chunks {
			if chunk.Page > maxPage {
				maxPage = chunk.Page
			}
			if onChunk != nil {
				if err := onChunk(chunk); err != nil {
					return 0, 0, err
				}
			}
		}
		return len(doc.Chunks), maxPage, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return 0, 0, fmt.Errorf("topic extractor stream failed with status %d: %s", resp.StatusCode, string(raw))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	count := 0
	maxPage := 0
	index := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return count, maxPage, fmt.Errorf("failed to parse stream event: %w", err)
		}
		switch event.Type {
		case "error":
			return count, maxPage, fmt.Errorf("topic extractor stream error: %s", event.Error)
		case "chunk":
			if event.Chunk == nil {
				continue
			}
			text := strings.TrimSpace(event.Chunk.Text)
			if text == "" {
				continue
			}
			sum := sha256.Sum256([]byte(text))
			page := event.Chunk.PageNumber
			if page <= 0 {
				page = 1
			}
			if page > maxPage {
				maxPage = page
			}
			chunk := model.Chunk{
				ID:       staged.FileID + "-" + shortHash(sum[:]) + "-" + strconv.Itoa(index),
				FileID:   staged.FileID,
				FileName: staged.OriginalName,
				FileKind: staged.DetectedKind,
				ChatID:   staged.ChatID,
				UserID:   staged.UserID,
				TopicID:  staged.TopicID,
				Page:     page,
				Index:    index,
				Text:     text,
				Hash:     hex.EncodeToString(sum[:]),
				Metadata: map[string]interface{}{
					"section_title":   strings.TrimSpace(event.Chunk.SectionTitle),
					"page_range":      strings.TrimSpace(event.Chunk.PageRange),
					"chunk_type":      strings.TrimSpace(firstNonEmpty(event.Chunk.ChunkType, event.Chunk.Type, "text")),
					"code_language":   strings.TrimSpace(event.Chunk.CodeLanguage),
					"formula_latex":   strings.TrimSpace(event.Chunk.FormulaLatex),
					"has_formula":     event.Chunk.HasFormula,
					"picture_class":   strings.TrimSpace(event.Chunk.PictureClass),
					"picture_caption": strings.TrimSpace(event.Chunk.PictureCaption),
					"page_from":       event.Chunk.PageFrom,
					"page_to":         event.Chunk.PageTo,
					"chunk_idx":       index,
				},
			}
			if onChunk != nil {
				if err := onChunk(chunk); err != nil {
					return count, maxPage, err
				}
			}
			count++
			index++
		}
	}
	if err := scanner.Err(); err != nil {
		return count, maxPage, err
	}
	return count, maxPage, nil
}

func (c *HTTPClient) StartTopicJob(ctx context.Context, jobID string, staged model.StagedFile) error {
	fileData, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return err
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("job_id", strings.TrimSpace(jobID)); err != nil {
		return err
	}
	if err := writer.WriteField("file_id", strings.TrimSpace(staged.FileID)); err != nil {
		return err
	}
	if err := writer.WriteField("topic_id", strings.TrimSpace(staged.TopicID)); err != nil {
		return err
	}
	if err := writer.WriteField("title", strings.TrimSpace(staged.OriginalName)); err != nil {
		return err
	}
	if err := writer.WriteField("pages_per_batch", strconv.Itoa(c.pagesPerBatch)); err != nil {
		return err
	}
	if err := writer.WriteField("page_overlap", strconv.Itoa(c.pageOverlap)); err != nil {
		return err
	}
	if err := writer.WriteField("max_chars", strconv.Itoa(c.maxChars)); err != nil {
		return err
	}
	if err := writer.WriteField("overlap_paragraphs", strconv.Itoa(c.overlapParas)); err != nil {
		return err
	}
	part, err := writer.CreateFormFile("file", filepath.Base(staged.OriginalName))
	if err != nil {
		return err
	}
	if _, err := part.Write(fileData); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jobs/start", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("topic extractor start failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func shortHash(hash []byte) string {
	enc := hex.EncodeToString(hash)
	if len(enc) <= 12 {
		return enc
	}
	return enc[:12]
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
