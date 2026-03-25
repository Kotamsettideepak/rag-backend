package gemini

import (
	"path/filepath"
	"strings"

	"gin-backend/model"
)

func buildImageTexts(staged model.StagedFile, response imageAnalysisResult) (string, []string) {
	metadata := buildImageMetadataBlock(staged)
	analysis := buildImageAnalysisBlock(response)
	pageTexts := []string{metadata}
	if analysis != "" {
		pageTexts = append(pageTexts, analysis)
	}
	text := strings.TrimSpace(strings.Join(pageTexts, "\n\n"))
	if text == "" {
		text = metadata
	}
	return text, pageTexts
}

func buildImageMetadataBlock(staged model.StagedFile) string {
	return strings.TrimSpace(strings.Join([]string{
		"Uploaded Image Metadata",
		"Actual uploaded filename: " + strings.TrimSpace(staged.OriginalName),
		"Image format: " + strings.ToUpper(strings.TrimPrefix(strings.ToLower(filepath.Ext(staged.OriginalName)), ".")),
		"Detected file type: " + strings.ToUpper(strings.TrimSpace(staged.DetectedKind)),
		"Content-Type: " + strings.TrimSpace(staged.ContentType),
	}, "\n"))
}

func buildImageAnalysisBlock(response imageAnalysisResult) string {
	parts := make([]string, 0, 7)
	appendIf := func(label, value string) {
		if value != "" {
			parts = append(parts, label+value)
		}
	}
	if len(response.Objects) > 0 {
		appendIf("Objects present: ", strings.Join(cleanValues(response.Objects), ", "))
	}
	if len(response.Colors) > 0 {
		appendIf("Colors: ", strings.Join(cleanValues(response.Colors), ", "))
	}
	appendIf("Caption: ", strings.TrimSpace(response.Caption))
	if len(response.Relationships) > 0 {
		appendIf("Relationships: ", strings.Join(cleanValues(response.Relationships), "; "))
	}
	if len(response.TextInImage) > 0 {
		appendIf("Text in image: ", strings.Join(cleanValues(response.TextInImage), "; "))
	}
	appendIf("Detailed description: ", strings.TrimSpace(response.DetailedDescription))
	appendIf("Summary: ", strings.TrimSpace(response.ContextSummary))
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func cleanValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
