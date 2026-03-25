package gemini

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseGeminiResponse(response generateContentResponse) (imageAnalysisResult, error) {
	if len(response.Candidates) == 0 {
		return imageAnalysisResult{}, fmt.Errorf("gemini returned no candidates")
	}
	rawOutput := extractCandidateText(response.Candidates[0])
	if strings.TrimSpace(rawOutput) == "" {
		return imageAnalysisResult{}, fmt.Errorf("gemini returned empty analysis text")
	}
	parsed, err := parseAnalysisJSON(rawOutput)
	if err != nil {
		return imageAnalysisResult{}, err
	}
	return imageAnalysisResult{
		DetailedDescription: normalizeString(parsed["detailed_description"]),
		Objects:             normalizeStringSlice(parsed["objects"]),
		Colors:              normalizeStringSlice(parsed["colors"]),
		Caption:             normalizeString(parsed["caption"]),
		Relationships:       normalizeStringSlice(parsed["relationships"]),
		TextInImage:         normalizeStringSlice(parsed["text_in_image"]),
		ContextSummary:      normalizeString(parsed["context_summary"]),
	}, nil
}

func extractCandidateText(candidate geminiCandidate) string {
	parts := make([]string, 0, len(candidate.Content.Parts))
	for _, part := range candidate.Content.Parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func parseAnalysisJSON(raw string) (map[string]any, error) {
	cleaned := cleanModelOutput(raw)
	candidates := []string{cleaned}
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		candidates = append(candidates, cleaned[start:end+1])
	}
	for _, candidate := range candidates {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(candidate), &parsed); err == nil {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("gemini returned non-JSON analysis: %s", raw)
}

func cleanModelOutput(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}
	return strings.TrimSpace(cleaned)
}

func normalizeString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(fmt.Sprintf("%v", value)), " "))
}

func normalizeStringSlice(value any) []string {
	switch typed := value.(type) {
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := normalizeString(item); text != "" {
				items = append(items, text)
			}
		}
		return items
	case []string:
		return cleanValues(typed)
	case string:
		if text := normalizeString(typed); text != "" {
			return []string{text}
		}
	}
	return nil
}
