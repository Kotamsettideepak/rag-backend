package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gin-backend/config"
	"gin-backend/model"
)

type Client interface {
	Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error)
}

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

type documentElement struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	Page      int    `json:"page"`
	Indexable bool   `json:"indexable"`
}

type extractResponse struct {
	Elements []documentElement `json:"elements"`
}

func NewClient() Client {
	return &HTTPClient{
		baseURL: config.GetExtractorBaseURL(),
		client:  &http.Client{Timeout: 10 * time.Minute},
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error) {
	log.Printf(
		"[extractor] preparing request file=%s kind=%s content_type=%s path=%s model=PyMuPDF/FastAPI",
		staged.OriginalName,
		staged.DetectedKind,
		staged.ContentType,
		staged.StoredPath,
	)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	var err error

	if err := writer.WriteField("file_kind", staged.DetectedKind); err != nil {
		return model.ParsedDocument{}, err
	}
	if err := writer.WriteField("original_name", staged.OriginalName); err != nil {
		return model.ParsedDocument{}, err
	}
	if err := writer.WriteField("content_type", staged.ContentType); err != nil {
		return model.ParsedDocument{}, err
	}

	fileData := []byte(nil)
	if strings.TrimSpace(staged.StoredPath) != "" {
		fileData, err = os.ReadFile(staged.StoredPath)
		if err != nil {
			return model.ParsedDocument{}, err
		}

		part, err := writer.CreateFormFile("file", filepath.Base(staged.OriginalName))
		if err != nil {
			return model.ParsedDocument{}, err
		}
		if _, err := part.Write(fileData); err != nil {
			return model.ParsedDocument{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return model.ParsedDocument{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/extract", body)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	log.Printf(
		"[extractor] sending request url=%s file=%s bytes=%d",
		c.baseURL+"/extract",
		staged.OriginalName,
		len(fileData),
	)

	resp, err := c.client.Do(req)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	log.Printf("[extractor] response status=%d file=%s body_bytes=%d", resp.StatusCode, staged.OriginalName, len(responseBody))
	if resp.StatusCode != http.StatusOK {
		return model.ParsedDocument{}, fmt.Errorf("extractor failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed extractResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return model.ParsedDocument{}, err
	}

	document := model.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  strings.TrimSpace(staged.DetectedKind),
		Text:      buildDocumentText(staged, parsed.Elements),
		PageTexts: buildPageTexts(staged, parsed.Elements),
		ChatID:    staged.ChatID,
		UserID:    staged.UserID,
	}
	log.Printf(
		"[extractor] extracted file=%s elements=%d pages=%d text_chars=%d preview=%s",
		staged.OriginalName,
		len(parsed.Elements),
		len(document.PageTexts),
		len(document.Text),
		previewText(document.Text, 220),
	)
	return document, nil
}
