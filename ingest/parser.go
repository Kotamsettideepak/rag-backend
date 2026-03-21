package ingest

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gin-backend/models"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) StageFiles(files []*multipart.FileHeader) ([]models.StagedFile, error) {
	if err := os.MkdirAll("./temp", 0o755); err != nil {
		return nil, err
	}

	staged := make([]models.StagedFile, 0, len(files))
	for index, file := range files {
		detectedKind := detectKind(file.Filename, file.Header.Get("Content-Type"))
		log.Printf(
			"[parser] file=%s content_type=%s detected_kind=%s size=%d",
			file.Filename,
			file.Header.Get("Content-Type"),
			detectedKind,
			file.Size,
		)
		if !isSupportedKind(detectedKind) {
			return nil, fmt.Errorf("only PDF, audio, and image files are supported right now: %s", file.Filename)
		}

		fileID := generateID()
		storedName := fileID + "_" + sanitizeFilename(file.Filename)
		storedPath := filepath.Join(".", "temp", storedName)

		if err := saveMultipartToPath(file, storedPath); err != nil {
			return nil, err
		}

		staged = append(staged, models.StagedFile{
			FileID:        fileID,
			OriginalName:  file.Filename,
			StoredPath:    storedPath,
			Size:          file.Size,
			ContentType:   file.Header.Get("Content-Type"),
			DetectedKind:  detectedKind,
			OriginalOrder: index,
		})
	}

	return staged, nil
}

func (p *Parser) StageYouTubeURL(rawURL string) ([]models.StagedFile, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("youtube url is required")
	}
	if err := validateYouTubeURL(rawURL); err != nil {
		return nil, err
	}

	fileID := generateID()
	displayName := deriveYouTubeDisplayName(rawURL)
	staged := []models.StagedFile{
		{
			FileID:        fileID,
			OriginalName:  displayName,
			SourceURL:     rawURL,
			ContentType:   "text/url",
			DetectedKind:  KindYouTube,
			OriginalOrder: 0,
		},
	}

	log.Printf("[parser] url=%s detected_kind=%s display_name=%s", rawURL, KindYouTube, displayName)
	return staged, nil
}

func (p *Parser) Cleanup(staged []models.StagedFile) {
	for _, file := range staged {
		if strings.TrimSpace(file.StoredPath) == "" {
			continue
		}
		if err := os.Remove(file.StoredPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[parser] failed to remove staged file %s: %v", file.StoredPath, err)
		}
	}
}

func saveMultipartToPath(file *multipart.FileHeader, path string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(path)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

func validateYouTubeURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid youtube url")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("invalid youtube url")
	}

	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be", "www.youtu.be":
		return nil
	default:
		return fmt.Errorf("only youtube links are supported")
	}
}

func deriveYouTubeDisplayName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "YouTube video"
	}

	videoID := strings.TrimSpace(parsed.Query().Get("v"))
	if videoID == "" {
		videoID = strings.Trim(strings.TrimSpace(parsed.Path), "/")
	}
	if videoID == "" {
		return "YouTube video"
	}

	return "YouTube video: " + videoID
}
