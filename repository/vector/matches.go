package vector

import (
	"fmt"
	"strings"

	"gin-backend/model"
)

func buildSearchMatches(response queryResponse) []model.SearchMatch {
	if len(response.Documents) == 0 {
		return nil
	}
	documents := response.Documents[0]
	ids := firstIDGroup(response.IDs)
	metadatas := firstMetadataGroup(response.Metadatas)
	matches := make([]model.SearchMatch, 0, len(documents))
	for index, document := range documents {
		match := model.SearchMatch{Document: document}
		if index < len(ids) {
			match.ID = ids[index]
		}
		if index < len(metadatas) {
			match.Metadata = metadatas[index]
		}
		matches = append(matches, match)
	}
	return matches
}

func buildGetMatches(response getResponse) []model.SearchMatch {
	matches := make([]model.SearchMatch, 0, len(response.Documents))
	for index, document := range response.Documents {
		match := model.SearchMatch{Document: document}
		if index < len(response.IDs) {
			match.ID = response.IDs[index]
		}
		if index < len(response.Metadatas) {
			match.Metadata = response.Metadatas[index]
		}
		matches = append(matches, match)
	}
	return matches
}

func firstIDGroup(groups [][]string) []string {
	if len(groups) == 0 {
		return nil
	}
	return groups[0]
}

func firstMetadataGroup(groups [][]map[string]interface{}) []map[string]interface{} {
	if len(groups) == 0 {
		return nil
	}
	return groups[0]
}

func firstMatchMetadata(matches []model.SearchMatch, key string) string {
	if len(matches) == 0 || matches[0].Metadata == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", matches[0].Metadata[key]))
}

func firstMatchPreview(matches []model.SearchMatch) string {
	if len(matches) == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(strings.TrimSpace(matches[0].Document)), " ")
	if len(text) <= 180 {
		return text
	}
	return text[:180] + "..."
}
