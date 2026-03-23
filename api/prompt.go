package api

import (
	"fmt"
	"strings"
)

const (
	contextModalityPDF    = "pdf"
	contextModalityAudio  = "audio"
	contextModalityImage  = "image"
	contextModalityMixed  = "mixed"
	recentContextMessages = 5
	maxConversationChars  = 3000
	maxContextChars       = 9000
	maxQuestionChars      = 1000
)

func buildPrompt(modality string, contextText string, conversationText string, question string) string {
	conversationText = clampPromptSection(conversationText, maxConversationChars)
	contextText = clampPromptSection(contextText, maxContextChars)
	question = clampPromptSection(question, maxQuestionChars)

	sections := []string{
		"You are answering questions only from the uploaded content.",
		fmt.Sprintf("Context modality: %s", normalizeModality(modality)),
		fmt.Sprintf("Conversation history:\n%s", strings.TrimSpace(conversationText)),
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
			"- For questions about what a song is about, its meaning, theme, mood, or story, synthesize the overall transcript instead of relying on one isolated lyric line.",
			"- When answering high-level song questions, summarize in plain language first and use short lyric references only as supporting evidence.",
		}
	case contextModalityPDF:
		modalityRules = []string{
			"- Prefer explicit uploaded file metadata for file name, file type, extension, or document identity questions.",
			"- Do not confuse the uploaded filename with titles, internal chapter names, print marks, or layout/source file names appearing inside the document.",
			"- Use the document structure and headings when they help answer the question accurately.",
		}
	case contextModalityImage:
		modalityRules = []string{
			"- Answer from visible image details, generated captions, summaries, and uploaded image metadata only.",
			"- Do not infer hidden actions, identities, brands, or scene details unless they are clearly present in the retrieved context.",
			"- Prefer the retrieved objects, colors, caption, and summary when describing the image.",
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
	case contextModalityImage:
		return contextModalityImage
	default:
		return contextModalityMixed
	}
}

func clampPromptSection(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}

	cutoff := strings.LastIndex(text[:limit], "\n\n")
	if cutoff < limit/2 {
		cutoff = strings.LastIndex(text[:limit], "\n")
	}
	if cutoff < limit/2 {
		cutoff = strings.LastIndex(text[:limit], ". ")
	}
	if cutoff < limit/2 {
		cutoff = limit
	}

	return strings.TrimSpace(text[:cutoff]) + "\n\n[Context truncated for model size.]"
}
