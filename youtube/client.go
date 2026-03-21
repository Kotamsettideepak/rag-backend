package youtube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gin-backend/audio"
	"gin-backend/config"
	"gin-backend/models"
)

type Client interface {
	Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error)
}

type HTTPClient struct {
	baseURL     string
	client      *http.Client
	audioClient audio.Client
}

func NewHTTPClient() Client {
	return &HTTPClient{
		baseURL:     config.GetExtractorBaseURL(),
		client:      &http.Client{Timeout: 10 * time.Minute},
		audioClient: audio.NewHTTPClient(),
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error) {
	if strings.TrimSpace(staged.SourceURL) == "" {
		return models.ParsedDocument{}, fmt.Errorf("youtube source url is required")
	}

	log.Printf(
		"[youtube] preparing download url=%s file=%s model=yt-dlp",
		staged.SourceURL,
		staged.OriginalName,
	)

	audioData, downloadedName, err := c.downloadAudio(ctx, staged.SourceURL)
	if err != nil {
		return models.ParsedDocument{}, err
	}

	tempFile, err := os.CreateTemp("./temp", "youtube-*.mp3")
	if err != nil {
		return models.ParsedDocument{}, err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(audioData); err != nil {
		return models.ParsedDocument{}, err
	}

	audioName := strings.TrimSpace(downloadedName)
	if audioName == "" {
		audioName = sanitizeDownloadedName(staged.OriginalName)
	}

	audioStaged := models.StagedFile{
		FileID:        staged.FileID,
		OriginalName:  audioName,
		StoredPath:    tempPath,
		SourceURL:     staged.SourceURL,
		Size:          int64(len(audioData)),
		ContentType:   "audio/mpeg",
		DetectedKind:  "audio",
		OriginalOrder: staged.OriginalOrder,
	}

	return c.audioClient.Extract(ctx, audioStaged)
}

func (c *HTTPClient) downloadAudio(ctx context.Context, sourceURL string) ([]byte, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("url", sourceURL); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	endpoint := c.baseURL + "/download-youtube-audio"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	log.Printf("[youtube] sending extractor request url=%s source_url=%s", endpoint, sourceURL)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	log.Printf("[youtube] response status=%d body_bytes=%d", resp.StatusCode, len(responseBody))
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("youtube audio download failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	return responseBody, resp.Header.Get("X-Downloaded-Filename"), nil
}

func sanitizeDownloadedName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" {
		return "youtube-audio.mp3"
	}
	if filepath.Ext(name) == "" {
		return name + ".mp3"
	}
	return name
}
