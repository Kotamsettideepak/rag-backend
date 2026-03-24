package ingest

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"gin-backend/models"
	"gin-backend/storage"
)

type Parser struct {
	cloudinary *storage.CloudinaryClient
}

func NewParser() *Parser {
	return &Parser{
		cloudinary: storage.NewCloudinaryClient(),
	}
}

func (p *Parser) StageFiles(files []*multipart.FileHeader, chatID string, userID string) ([]models.StagedFile, error) {
	if err := os.MkdirAll("./temp", 0o755); err != nil {
		return nil, err
	}

	if p.cloudinary == nil || !p.cloudinary.Enabled() {
		log.Printf("[parser] cloudinary is not configured; uploaded files will not have cloud urls")
	}

	staged := make([]models.StagedFile, 0, len(files))
	pdfCount := 0
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
			return nil, fmt.Errorf("only PDF, audio, video, and image files are supported right now: %s", file.Filename)
		}
		if detectedKind == KindPDF {
			pdfCount++
			if pdfCount > 1 {
				return nil, fmt.Errorf("only one PDF can be uploaded at a time")
			}
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
			ChatID:        chatID,
			UserID:        userID,
		})
	}

	return staged, nil
}

func (p *Parser) AttachCloudURL(ctx context.Context, staged *models.StagedFile) error {
	if staged == nil || strings.TrimSpace(staged.StoredPath) == "" {
		return nil
	}
	if p.cloudinary == nil || !p.cloudinary.Enabled() {
		return nil
	}

	payload, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return err
	}

	uploadedURL, err := p.cloudinary.Upload(ctx, staged.OriginalName, payload)
	if err != nil {
		return err
	}

	staged.CloudURL = uploadedURL
	return nil
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
