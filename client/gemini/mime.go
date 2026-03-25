package gemini

import (
	"mime"
	"path/filepath"
	"slices"
	"strings"

	"gin-backend/model"
)

func detectImageMIMEType(staged model.StagedFile) string {
	if contentType := strings.TrimSpace(staged.ContentType); strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	extension := strings.ToLower(filepath.Ext(staged.OriginalName))
	if extension != "" {
		if contentType := mime.TypeByExtension(extension); strings.HasPrefix(contentType, "image/") {
			return contentType
		}
	}
	if slices.Contains([]string{".jpg", ".jpeg"}, extension) {
		return "image/jpeg"
	}
	return "image/png"
}
