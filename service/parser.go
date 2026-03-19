package service

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"gin-backend/models"

	"github.com/ledongthuc/pdf"
)

type Parser struct {
	ollama *models.OllamaClient
}

func NewParser(ollama *models.OllamaClient) *Parser {
	return &Parser{ollama: ollama}
}

func (p *Parser) StageFiles(files []*multipart.FileHeader) ([]models.StagedFile, error) {
	if err := os.MkdirAll("./temp", 0o755); err != nil {
		return nil, err
	}

	staged := make([]models.StagedFile, 0, len(files))
	for index, file := range files {
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
			DetectedKind:  detectKind(file.Filename, file.Header.Get("Content-Type")),
			OriginalOrder: index,
		})
	}

	return staged, nil
}

func (p *Parser) ParseFile(staged models.StagedFile) (models.ParsedDocument, error) {
	switch staged.DetectedKind {
	case "pdf":
		return p.parsePDF(staged)
	case "image":
		return p.parseImage(staged)
	case "audio":
		return p.parsePlaceholder(staged, "Audio transcription placeholder for file "+staged.OriginalName+". Replace this with Whisper or another speech-to-text pipeline.")
	case "video":
		return p.parsePlaceholder(staged, "Video transcript placeholder for file "+staged.OriginalName+". Replace this with a video/audio extraction pipeline.")
	default:
		return models.ParsedDocument{}, fmt.Errorf("unsupported file type: %s", staged.OriginalName)
	}
}

func (p *Parser) Cleanup(staged []models.StagedFile) {
	for _, file := range staged {
		if err := os.Remove(file.StoredPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[parser] failed to remove staged file %s: %v", file.StoredPath, err)
		}
	}
}

func (p *Parser) parsePDF(staged models.StagedFile) (models.ParsedDocument, error) {
	file, reader, err := pdf.Open(staged.StoredPath)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	defer file.Close()

	pageTexts := make([]string, 0, reader.NumPage())
	fullText := strings.Builder{}

	for pageNumber := 1; pageNumber <= reader.NumPage(); pageNumber++ {
		page := reader.Page(pageNumber)
		if page.V.IsNull() {
			continue
		}

		pageText, _ := page.GetPlainText(nil)
		pageText = normalizeText(pageText)
		if pageText == "" {
			continue
		}

		pageTexts = append(pageTexts, pageText)
		if fullText.Len() > 0 {
			fullText.WriteString("\n\n")
		}
		fullText.WriteString(pageText)
	}

	return models.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  staged.DetectedKind,
		Text:      fullText.String(),
		PageTexts: pageTexts,
	}, nil
}

func (p *Parser) parseImage(staged models.StagedFile) (models.ParsedDocument, error) {
	fileData, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return models.ParsedDocument{}, err
	}

	imageBase64 := base64.StdEncoding.EncodeToString(fileData)
	description, err := p.ollama.DescribeImage("Describe this image in detail.", imageBase64)
	if err != nil {
		return models.ParsedDocument{}, err
	}

	description = normalizeText(description)
	return models.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  staged.DetectedKind,
		Text:      description,
		PageTexts: []string{description},
	}, nil
}

func (p *Parser) parsePlaceholder(staged models.StagedFile, text string) (models.ParsedDocument, error) {
	text = normalizeText(text)
	return models.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  staged.DetectedKind,
		Text:      text,
		PageTexts: []string{text},
	}, nil
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
	case strings.HasSuffix(lowerName, ".pdf"):
		return "pdf"
	case strings.HasPrefix(lowerType, "image/"), strings.HasSuffix(lowerName, ".png"), strings.HasSuffix(lowerName, ".jpg"), strings.HasSuffix(lowerName, ".jpeg"):
		return "image"
	case strings.HasPrefix(lowerType, "audio/"), strings.HasSuffix(lowerName, ".mp3"), strings.HasSuffix(lowerName, ".wav"):
		return "audio"
	case strings.HasPrefix(lowerType, "video/"), strings.HasSuffix(lowerName, ".mp4"), strings.HasSuffix(lowerName, ".mov"):
		return "video"
	default:
		return "unknown"
	}
}
