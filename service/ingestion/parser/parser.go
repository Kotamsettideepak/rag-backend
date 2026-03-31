package parser

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"gin-backend/client/cloudinary"
	"gin-backend/model"

	"github.com/google/uuid"
)

const (
	KindPDF   = "pdf"
	KindAudio = "audio"
	KindImage = "image"
	KindVideo = "video"

	maxVideoUploadBytes = 150 << 20
	maxImageUploads     = 3
)

// Parser stages uploaded files to disk and optionally uploads to Cloudinary.
type Parser struct {
	cloudinary *cloudinary.Client
}

// New creates a Parser with Cloudinary support.
func New() *Parser {
	return &Parser{cloudinary: cloudinary.NewClient()}
}

// StageChatFiles writes chat-upload files to ./temp and returns StagedFile metadata.
func (p *Parser) StageChatFiles(files []*multipart.FileHeader, chatID, userID string) ([]model.StagedFile, error) {
	if err := os.MkdirAll("./temp", 0o755); err != nil {
		return nil, err
	}

	staged := make([]model.StagedFile, 0, len(files))
	pdfCount := 0
	imageCount := 0
	selectedKind := ""
	for i, file := range files {
		kind := detectKind(file.Filename, file.Header.Get("Content-Type"))
		if !isSupportedKind(kind) {
			return nil, fmt.Errorf("unsupported file type: %s", file.Filename)
		}
		if selectedKind == "" {
			selectedKind = kind
		} else if kind != selectedKind {
			return nil, fmt.Errorf("only one file format can be uploaded at a time")
		}
		if kind == KindPDF {
			pdfCount++
			if pdfCount > 1 {
				return nil, fmt.Errorf("only one PDF can be uploaded at a time")
			}
		}
		if kind == KindImage {
			imageCount++
			if imageCount > maxImageUploads {
				return nil, fmt.Errorf("upload up to %d images at a time", maxImageUploads)
			}
		}
		if kind == KindVideo && file.Size > maxVideoUploadBytes {
			return nil, fmt.Errorf("upload video less than 150 MB")
		}
		fileID := uuid.NewString()
		storedName := fileID + "_" + sanitize(file.Filename)
		storedPath := filepath.Join(".", "temp", storedName)
		if err := saveMultipart(file, storedPath); err != nil {
			return nil, err
		}
		staged = append(staged, model.StagedFile{
			FileID: fileID, OriginalName: file.Filename, StoredPath: storedPath,
			Size: file.Size, ContentType: file.Header.Get("Content-Type"),
			DetectedKind: kind, OriginalOrder: i, ChatID: chatID, UserID: userID,
		})
	}
	return staged, nil
}

// AttachCloudURL uploads the file to Cloudinary and sets CloudURL on the StagedFile.
func (p *Parser) AttachCloudURL(ctx context.Context, staged *model.StagedFile) error {
	if staged == nil || staged.StoredPath == "" {
		return nil
	}
	if p.cloudinary == nil || !p.cloudinary.Enabled() {
		return nil
	}
	data, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return err
	}
	url, err := p.cloudinary.Upload(ctx, staged.OriginalName, data)
	if err != nil {
		return err
	}
	staged.CloudURL = url
	return nil
}

// Cleanup removes temporary files from disk.
func (p *Parser) Cleanup(staged []model.StagedFile) {
	for _, f := range staged {
		if f.StoredPath == "" {
			continue
		}
		if err := os.Remove(f.StoredPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[parser] cleanup failed path=%s err=%v", f.StoredPath, err)
		}
	}
}

func saveMultipart(file *multipart.FileHeader, path string) error {
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

func sanitize(name string) string {
	name = filepath.Base(name)
	return strings.ReplaceAll(name, " ", "_")
}

var audioExts = []string{".mp3", ".wav", ".m4a", ".aac", ".ogg", ".flac", ".webm"}
var videoExts = []string{".mp4", ".mov", ".avi", ".mkv", ".m4v", ".mpeg", ".mpg", ".wmv", ".3gp"}
var imageExts = []string{".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif"}

func detectKind(filename, contentType string) string {
	name := strings.ToLower(strings.TrimSpace(filename))
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasSuffix(name, ".pdf"), strings.Contains(ct, "pdf"):
		return KindPDF
	case isVideo(name, ct):
		return KindVideo
	case isAudio(name, ct):
		return KindAudio
	case isImage(name, ct):
		return KindImage
	}
	return "unknown"
}

func isSupportedKind(k string) bool {
	return k == KindPDF || k == KindAudio || k == KindImage || k == KindVideo
}

func isVideo(name, ct string) bool {
	if strings.HasPrefix(ct, "video/") {
		return true
	}
	for _, ext := range videoExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func isAudio(name, ct string) bool {
	if strings.HasPrefix(ct, "audio/") {
		return true
	}
	for _, ext := range audioExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func isImage(name, ct string) bool {
	if strings.HasPrefix(ct, "image/") {
		return true
	}
	for _, ext := range imageExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
