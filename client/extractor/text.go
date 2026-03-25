package extractor

import (
	"sort"
	"strings"

	"gin-backend/model"
)

func buildDocumentText(staged model.StagedFile, elements []documentElement) string {
	parts := []string{buildFileMetadataBlock(staged)}
	parts = append(parts, collectElementText(elements)...)
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildPageTexts(staged model.StagedFile, elements []documentElement) []string {
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

func buildFileMetadataBlock(staged model.StagedFile) string {
	lines := []string{
		"Uploaded File Metadata",
		"Actual uploaded filename: " + strings.TrimSpace(staged.OriginalName),
		"Detected file type: " + strings.ToUpper(strings.TrimSpace(staged.DetectedKind)),
		"Content-Type: " + strings.TrimSpace(staged.ContentType),
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func groupPageTexts(elements []documentElement) []string {
	pageMap := make(map[int][]string)
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
		pageMap[page] = append(pageMap[page], content)
	}
	if len(pageMap) == 0 {
		return nil
	}
	pageNumbers := make([]int, 0, len(pageMap))
	for page := range pageMap {
		pageNumbers = append(pageNumbers, page)
	}
	sort.Ints(pageNumbers)
	pages := make([]string, 0, len(pageNumbers))
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
