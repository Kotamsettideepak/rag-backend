package groq

import (
	"fmt"
	"strings"

	"gin-backend/model"
)

func buildAudioTexts(staged model.StagedFile, duration float64, chunks []model.AudioTranscriptChunk) (string, []string) {
	metadata := buildAudioMetadataBlock(staged, duration)
	parts := []string{metadata}
	pageTexts := []string{metadata}
	for _, chunk := range chunks {
		if entry := formatAudioTranscriptLine(chunk); entry != "" {
			parts = append(parts, entry)
			pageTexts = append(pageTexts, entry)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if text == "" {
		text = metadata
	}
	return text, pageTexts
}

func buildAudioMetadataBlock(staged model.StagedFile, duration float64) string {
	durationLine := "Estimated duration: unavailable"
	if duration > 0 {
		durationLine = fmt.Sprintf("Estimated duration: %.2f seconds", duration)
	}
	lines := []string{
		"Uploaded Audio Metadata",
		"Actual uploaded filename: " + strings.TrimSpace(staged.OriginalName),
		"Detected file type: " + strings.ToUpper(strings.TrimSpace(staged.DetectedKind)),
		"Content-Type: " + strings.TrimSpace(staged.ContentType),
		durationLine,
	}
	if strings.TrimSpace(staged.SourceURL) != "" {
		lines = append(lines, "Source URL: "+strings.TrimSpace(staged.SourceURL))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildTranscriptChunk(window audioWindow, response transcriptionResponse) (model.AudioTranscriptChunk, bool) {
	if len(response.Segments) == 0 {
		return model.AudioTranscriptChunk{}, false
	}
	texts := make([]string, 0, len(response.Segments))
	start := -1.0
	end := 0.0
	for _, segment := range response.Segments {
		if text := strings.TrimSpace(segment.Text); text != "" {
			texts = append(texts, text)
			if start < 0 || segment.Start < start {
				start = segment.Start
			}
			if segment.End > end {
				end = segment.End
			}
		}
	}
	if len(texts) == 0 {
		return model.AudioTranscriptChunk{}, false
	}
	fullText := strings.Join(strings.Fields(strings.TrimSpace(strings.Join(texts, " "))), " ")
	if len(fullText) < minTranscriptChars {
		return model.AudioTranscriptChunk{}, false
	}
	absoluteStart := window.Start
	if start >= 0 {
		absoluteStart += start
	}
	absoluteEnd := window.End
	if end > 0 {
		absoluteEnd = window.Start + end
	}
	if absoluteEnd < absoluteStart {
		absoluteEnd = absoluteStart
	}
	return model.AudioTranscriptChunk{Content: fullText, Start: absoluteStart, End: absoluteEnd, Type: "audio_transcript"}, true
}

func buildChunksFromResponse(response transcriptionResponse) []model.AudioTranscriptChunk {
	chunks := make([]model.AudioTranscriptChunk, 0, len(response.Segments))
	for _, segment := range response.Segments {
		text := strings.Join(strings.Fields(strings.TrimSpace(segment.Text)), " ")
		if len(text) >= minTranscriptChars {
			chunks = append(chunks, model.AudioTranscriptChunk{
				Content: text,
				Start:   segment.Start,
				End:     segment.End,
				Type:    "audio_transcript",
			})
		}
	}
	return mergeSmallTranscriptChunks(chunks)
}

func mergeSmallTranscriptChunks(chunks []model.AudioTranscriptChunk) []model.AudioTranscriptChunk {
	merged := make([]model.AudioTranscriptChunk, 0, len(chunks))
	var pending *model.AudioTranscriptChunk
	for _, chunk := range chunks {
		chunk.Content = strings.TrimSpace(chunk.Content)
		if chunk.Content == "" {
			continue
		}
		if pending != nil {
			chunk.Content = strings.TrimSpace(pending.Content + " " + chunk.Content)
			chunk.Start = pending.Start
			if chunk.Type == "" {
				chunk.Type = pending.Type
			}
			pending = nil
		}
		if len(chunk.Content) < minMergedChunkChars {
			copyChunk := chunk
			pending = &copyChunk
			continue
		}
		merged = append(merged, chunk)
	}
	if pending != nil {
		if len(merged) == 0 {
			merged = append(merged, *pending)
		} else {
			lastIndex := len(merged) - 1
			merged[lastIndex].Content = strings.TrimSpace(merged[lastIndex].Content + " " + pending.Content)
			if pending.End > merged[lastIndex].End {
				merged[lastIndex].End = pending.End
			}
		}
	}
	return merged
}

func formatAudioTranscriptLine(chunk model.AudioTranscriptChunk) string {
	if text := strings.TrimSpace(chunk.Content); text != "" {
		return fmt.Sprintf("[%s - %s] %s", formatTimestamp(chunk.Start), formatTimestamp(chunk.End), text)
	}
	return ""
}

func formatTimestamp(seconds float64) string {
	return fmt.Sprintf("%.2f", seconds)
}

func estimateAudioDuration(response transcriptionResponse) float64 {
	if response.Duration > 0 {
		return response.Duration
	}
	var maxEnd float64
	for _, segment := range response.Segments {
		if segment.End > maxEnd {
			maxEnd = segment.End
		}
	}
	return maxEnd
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
