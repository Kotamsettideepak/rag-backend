package retrieval

import (
	"fmt"
	"sort"
	"strings"

	"gin-backend/model"
)

const (
	ModalityPDF   = "pdf"
	ModalityAudio = "audio"
	ModalityVideo = "video"
	ModalityImage = "image"
	ModalityMixed = "mixed"
)

func classifyAudioQuery(question string) audioIntent {
	return classifyMediaQuery(question)
}

func classifyMediaQuery(question string) audioIntent {
	low := strings.ToLower(strings.TrimSpace(question))
	for _, kw := range []string{"lyrics", "lyric", "complete lyrics", "full lyrics"} {
		if strings.Contains(low, kw) {
			return intentLyrics
		}
	}
	for _, kw := range []string{
		"what is the song about", "what's the song about", "summarize the song", "summary of the song", "meaning of the song", "story of the song",
		"what is the video about", "what's the video about", "summarize the video", "summary of the video", "what happens in the video", "video about",
	} {
		if strings.Contains(low, kw) {
			return intentSummary
		}
	}
	for _, kw := range []string{"file name", "filename", "duration", "length", "song name", "title", "artist", "singer", "video name"} {
		if strings.Contains(low, kw) {
			return intentMetadata
		}
	}
	return intentSemantic
}

func filterByKind(matches []model.SearchMatch, kind string) []model.SearchMatch {
	return filterMatches(matches, func(match model.SearchMatch) bool {
		return strings.EqualFold(metaStr(match.Metadata, "file_kind"), kind)
	})
}

func filterByContentType(matches []model.SearchMatch, contentType string) []model.SearchMatch {
	return filterMatches(matches, func(match model.SearchMatch) bool {
		return strings.EqualFold(metaStr(match.Metadata, "content_type"), contentType)
	})
}

func sortAudio(matches []model.SearchMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		ls, lok := metaFloat(matches[i].Metadata, "segment_start")
		rs, rok := metaFloat(matches[j].Metadata, "segment_start")
		if lok && rok && ls != rs {
			return ls < rs
		}
		li, liok := metaInt(matches[i].Metadata, "chunk_idx")
		ri, riok := metaInt(matches[j].Metadata, "chunk_idx")
		if liok && riok && li != ri {
			return li < ri
		}
		return matches[i].Document < matches[j].Document
	})
}

func dedup(matches []model.SearchMatch) []model.SearchMatch {
	seen := make(map[string]struct{}, len(matches))
	out := make([]model.SearchMatch, 0, len(matches))
	for _, match := range matches {
		key := match.ID
		if key == "" {
			key = fmt.Sprintf("%s|%s", metaStr(match.Metadata, "file_id"), match.Document)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, match)
	}
	return out
}

func joinDocs(matches []model.SearchMatch) string {
	parts := make([]string, 0, len(matches))
	for _, match := range dedup(matches) {
		if doc := strings.TrimSpace(match.Document); doc != "" {
			parts = append(parts, doc)
		}
	}
	return joinParts(parts)
}

func joinParts(parts []string) string {
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
