package api

import (
	"fmt"
	"strings"
)

const (
	contextModalityPDF   = "pdf"
	contextModalityAudio = "audio"
	contextModalityMixed = "mixed"
)

func buildPrompt(modality string, contextText string, question string) string {
	sections := []string{
		"You are answering questions only from the uploaded content.",
		fmt.Sprintf("Context modality: %s", normalizeModality(modality)),
		fmt.Sprintf("Retrieved context:\n%s", strings.TrimSpace(contextText)),
		fmt.Sprintf("User question:\n%s", strings.TrimSpace(question)),
		"Instructions:\n" + buildInstructionBlock(modality),
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func buildInstructionBlock(modality string) string {
	baseRules := []string{
		"- Answer using only the retrieved context.",
		"- If the answer is not clearly present, say that it is not available in the uploaded content.",
		"- Be concise but complete.",
		"- When possible, quote exact values from the context.",
	}

	var modalityRules []string
	switch normalizeModality(modality) {
	case contextModalityAudio:
		modalityRules = []string{
			"- Prefer explicit uploaded audio metadata for filename, file type, duration, song title, writer, artist, singer, or composer questions.",
			"- Timestamp ranges like [12.50s - 18.20s] are transcript positions, not independent total durations.",
			"- Do not infer the full duration of an audio file from a single transcript segment.",
			"- If the user asks for complete lyrics or a complete transcript and the context only contains partial excerpts, clearly say the retrieved context is incomplete.",
		}
	case contextModalityPDF:
		modalityRules = []string{
			"- Prefer explicit uploaded file metadata for file name, file type, extension, or document identity questions.",
			"- Do not confuse the uploaded filename with titles, internal chapter names, print marks, or layout/source file names appearing inside the document.",
			"- Use the document structure and headings when they help answer the question accurately.",
		}
	default:
		modalityRules = []string{
			"- Prefer explicit uploaded file metadata for file identity questions.",
			"- If the context contains timestamps, treat them as excerpt positions unless the total duration is explicitly stated.",
		}
	}

	return strings.Join(append(baseRules, modalityRules...), "\n")
}

func normalizeModality(modality string) string {
	switch strings.ToLower(strings.TrimSpace(modality)) {
	case contextModalityAudio:
		return contextModalityAudio
	case contextModalityPDF:
		return contextModalityPDF
	default:
		return contextModalityMixed
	}
}
