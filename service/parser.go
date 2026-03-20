package service

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
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
		if detectedKind != "pdf" {
			return nil, fmt.Errorf("only PDF files are supported right now: %s", file.Filename)
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

func (p *Parser) Cleanup(staged []models.StagedFile) {
	for _, file := range staged {
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

func detectKind(filename string, contentType string) string {
	lowerName := strings.ToLower(filename)
	lowerType := strings.ToLower(contentType)

	switch {
	case strings.HasSuffix(lowerName, ".pdf"), strings.Contains(lowerType, "pdf"):
		return "pdf"
	default:
		return "unknown"
	}
}
