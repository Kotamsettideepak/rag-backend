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
	"sort"
	"strings"
	"time"

	"gin-backend/config"
	"gin-backend/models"
)

type Client interface {
	Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error)
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

func NewHTTPClient() Client {
	return &HTTPClient{
		baseURL: config.GetExtractorBaseURL(),
		client:  &http.Client{Timeout: 10 * time.Minute},
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error) {
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
		return models.ParsedDocument{}, err
	}
	if err := writer.WriteField("original_name", staged.OriginalName); err != nil {
		return models.ParsedDocument{}, err
	}
	if err := writer.WriteField("content_type", staged.ContentType); err != nil {
		return models.ParsedDocument{}, err
	}

	fileData := []byte(nil)
	if strings.TrimSpace(staged.StoredPath) != "" {
		fileData, err = os.ReadFile(staged.StoredPath)
		if err != nil {
			return models.ParsedDocument{}, err
		}

		part, err := writer.CreateFormFile("file", filepath.Base(staged.OriginalName))
		if err != nil {
			return models.ParsedDocument{}, err
		}
		if _, err := part.Write(fileData); err != nil {
			return models.ParsedDocument{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return models.ParsedDocument{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/extract", body)
	if err != nil {
		return models.ParsedDocument{}, err
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
		return models.ParsedDocument{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	log.Printf("[extractor] response status=%d file=%s body_bytes=%d", resp.StatusCode, staged.OriginalName, len(responseBody))
	if resp.StatusCode != http.StatusOK {
		return models.ParsedDocument{}, fmt.Errorf("extractor failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed extractResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return models.ParsedDocument{}, err
	}

	document := models.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  strings.TrimSpace(staged.DetectedKind),
		Text:      buildDocumentText(staged, parsed.Elements),
		PageTexts: buildPageTexts(staged, parsed.Elements),
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

func buildDocumentText(staged models.StagedFile, elements []documentElement) string {
	parts := []string{buildFileMetadataBlock(staged)}
	parts = append(parts, collectElementText(elements)...)
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildPageTexts(staged models.StagedFile, elements []documentElement) []string {
	pages := groupPageTexts(elements)
	if len(pages) == 0 {
		return []string{buildFileMetadataBlock(staged)}
	}

	pages[0] = strings.TrimSpace(buildFileMetadataBlock(staged) + "\n\n" + pages[0])
	return pages
}

func collectElementText(elements []documentElement) []string {
	parts := make([]string, 0, len(elements))
	for _, element := range elements {
		if !element.Indexable {
			continue
		}
		content := strings.TrimSpace(element.Content)
		if content != "" {
			parts = append(parts, content)
		}
	}
	return parts
}

func buildFileMetadataBlock(staged models.StagedFile) string {
	lines := []string{
		"Uploaded File Metadata",
		"Actual uploaded filename: " + strings.TrimSpace(staged.OriginalName),
		"Detected file type: " + strings.ToUpper(strings.TrimSpace(staged.DetectedKind)),
		"Content-Type: " + strings.TrimSpace(staged.ContentType),
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func groupPageTexts(elements []documentElement) []string {
	if len(elements) == 0 {
		return nil
	}

	pageMap := make(map[int][]string)
	maxPage := 0
	for _, element := range elements {
		if !element.Indexable {
			continue
		}
		content := strings.TrimSpace(element.Content)
		if content == "" {
			continue
		}

		page := element.Page
		if page <= 0 {
			page = 1
		}
		if page > maxPage {
			maxPage = page
		}
		pageMap[page] = append(pageMap[page], content)
	}

	if maxPage == 0 {
		return nil
	}

	pages := make([]string, 0, maxPage)
	pageNumbers := make([]int, 0, len(pageMap))
	for page := range pageMap {
		pageNumbers = append(pageNumbers, page)
	}
	sort.Ints(pageNumbers)

	for _, page := range pageNumbers {
		pages = append(pages, strings.TrimSpace(strings.Join(pageMap[page], "\n\n")))
	}

	return pages
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
