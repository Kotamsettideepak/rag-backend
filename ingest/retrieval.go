package ingest

import (
	"fmt"
	"sort"
	"strings"

	"gin-backend/db"
	"gin-backend/models"
)

const (
	searchModalityPDF   = "pdf"
	searchModalityAudio = "audio"
	searchModalityImage = "image"
	searchModalityMixed = "mixed"
)

func buildContextFromMatches(question string, matches []models.SearchMatch, store *db.ChromaStore) string {
	result := buildSearchContextResult(question, matches, store)
	return result.Context
}

func buildSearchContextResult(question string, matches []models.SearchMatch, store *db.ChromaStore) models.SearchContextResult {
	if len(matches) == 0 {
		return models.SearchContextResult{Context: "", Modality: searchModalityMixed}
	}

	audioMatches := filterMatchesByKind(matches, KindAudio)
	imageMatches := filterMatchesByKind(matches, KindImage)
	pdfMatches := filterMatchesByKind(matches, KindPDF)

	switch {
	case len(audioMatches) > 0 && len(imageMatches) == 0 && len(pdfMatches) == 0:
		fileScopedMatches := hydrateAudioMatches(audioMatches, store)
		intent := classifyAudioQuery(question)
		switch intent {
		case audioQueryMetadata:
			return models.SearchContextResult{
				Context:  buildAudioMetadataContext(fileScopedMatches),
				Modality: searchModalityAudio,
			}
		case audioQueryLyrics:
			return models.SearchContextResult{
				Context:  buildOrderedAudioTranscriptContext(fileScopedMatches),
				Modality: searchModalityAudio,
			}
		default:
			return models.SearchContextResult{
				Context:  buildAudioSemanticContext(fileScopedMatches, matches),
				Modality: searchModalityAudio,
			}
		}
	case len(imageMatches) > 0 && len(audioMatches) == 0 && len(pdfMatches) == 0:
		return models.SearchContextResult{
			Context:  joinMatchDocuments(matches),
			Modality: searchModalityImage,
		}
	case len(pdfMatches) > 0 && len(audioMatches) == 0 && len(imageMatches) == 0:
		return models.SearchContextResult{
			Context:  joinMatchDocuments(matches),
			Modality: searchModalityPDF,
		}
	default:
		return models.SearchContextResult{
			Context:  joinMatchDocuments(matches),
			Modality: searchModalityMixed,
		}
	}
}

func hydrateAudioMatches(matches []models.SearchMatch, store *db.ChromaStore) []models.SearchMatch {
	if len(matches) == 0 || store == nil {
		return matches
	}

	fileID := metadataString(matches[0].Metadata, "file_id")
	if fileID == "" {
		return matches
	}

	fullMatches, err := store.GetByMetadata(map[string]interface{}{
		"file_id": fileID,
	}, 512)
	if err != nil || len(fullMatches) == 0 {
		return matches
	}

	return filterMatchesByKind(fullMatches, KindAudio)
}

type audioQueryIntent int

const (
	audioQuerySemantic audioQueryIntent = iota
	audioQueryMetadata
	audioQueryLyrics
)

func classifyAudioQuery(question string) audioQueryIntent {
	lower := strings.ToLower(strings.TrimSpace(question))

	lyricsKeywords := []string{"lyrics", "lyric", "complete lyrics", "full lyrics"}
	for _, keyword := range lyricsKeywords {
		if strings.Contains(lower, keyword) {
			return audioQueryLyrics
		}
	}

	metadataKeywords := []string{
		"file name", "filename", "file type", "extension", "duration", "length",
		"song name", "title", "writer", "author", "artist", "singer", "composer",
	}
	for _, keyword := range metadataKeywords {
		if strings.Contains(lower, keyword) {
			return audioQueryMetadata
		}
	}

	return audioQuerySemantic
}

func buildAudioMetadataContext(matches []models.SearchMatch) string {
	metadataMatches := filterMatchesByContentType(matches, "audio_metadata")
	if len(metadataMatches) == 0 {
		return joinMatchDocuments(matches)
	}

	return joinMatchDocuments(metadataMatches)
}

func buildOrderedAudioTranscriptContext(matches []models.SearchMatch) string {
	transcriptMatches := filterMatchesByContentType(matches, "audio_transcript")
	if len(transcriptMatches) == 0 {
		return joinMatchDocuments(matches)
	}

	sortAudioMatches(transcriptMatches)
	return joinMatchDocuments(uniqueMatches(transcriptMatches))
}

func buildAudioSemanticContext(audioMatches []models.SearchMatch, original []models.SearchMatch) string {
	metadataMatches := filterMatchesByContentType(audioMatches, "audio_metadata")
	transcriptMatches := filterMatchesByContentType(audioMatches, "audio_transcript")

	sortAudioMatches(transcriptMatches)

	contextParts := make([]string, 0, len(original))
	if len(metadataMatches) > 0 {
		contextParts = append(contextParts, metadataMatches[0].Document)
	}

	for _, match := range uniqueMatches(transcriptMatches) {
		contextParts = append(contextParts, match.Document)
	}

	if len(contextParts) == 0 {
		return joinMatchDocuments(original)
	}

	return strings.TrimSpace(strings.Join(contextParts, "\n\n"))
}

func filterMatchesByKind(matches []models.SearchMatch, kind string) []models.SearchMatch {
	filtered := make([]models.SearchMatch, 0, len(matches))
	for _, match := range matches {
		if strings.EqualFold(metadataString(match.Metadata, "file_kind"), kind) {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

func filterMatchesByContentType(matches []models.SearchMatch, contentType string) []models.SearchMatch {
	filtered := make([]models.SearchMatch, 0, len(matches))
	for _, match := range matches {
		if strings.EqualFold(metadataString(match.Metadata, "content_type"), contentType) {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

func sortAudioMatches(matches []models.SearchMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		leftStart, leftOK := metadataFloat(matches[i].Metadata, "segment_start")
		rightStart, rightOK := metadataFloat(matches[j].Metadata, "segment_start")
		if leftOK && rightOK && leftStart != rightStart {
			return leftStart < rightStart
		}

		leftIdx, leftOK := metadataInt(matches[i].Metadata, "chunk_idx")
		rightIdx, rightOK := metadataInt(matches[j].Metadata, "chunk_idx")
		if leftOK && rightOK && leftIdx != rightIdx {
			return leftIdx < rightIdx
		}

		return matches[i].Document < matches[j].Document
	})
}

func uniqueMatches(matches []models.SearchMatch) []models.SearchMatch {
	seen := make(map[string]struct{}, len(matches))
	unique := make([]models.SearchMatch, 0, len(matches))
	for _, match := range matches {
		key := match.ID
		if key == "" {
			key = fmt.Sprintf("%s|%s", metadataString(match.Metadata, "file_id"), match.Document)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, match)
	}
	return unique
}

func joinMatchDocuments(matches []models.SearchMatch) string {
	parts := make([]string, 0, len(matches))
	for _, match := range uniqueMatches(matches) {
		document := strings.TrimSpace(match.Document)
		if document != "" {
			parts = append(parts, document)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func metadataFloat(metadata map[string]interface{}, key string) (float64, bool) {
	if metadata == nil {
		return 0, false
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0, false
	}

	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func metadataInt(metadata map[string]interface{}, key string) (int, bool) {
	if metadata == nil {
		return 0, false
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0, false
	}

	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}
