package prompt

import (
	"fmt"
	"strings"
)

const (
	ModalityPDF   = "pdf"
	ModalityAudio = "audio"
	ModalityImage = "image"
	ModalityMixed = "mixed"

	RecentContextMessages = 5
	MaxConversationChars  = 3000
	MaxContextChars       = 9000
	MaxQuestionChars      = 1000
)

// Build constructs the final LLM prompt from retrieval context and conversation history.
func Build(modality, contextText, conversationText, question string) string {
	conversationText = clamp(conversationText, MaxConversationChars)
	contextText = clamp(contextText, MaxContextChars)
	question = clamp(question, MaxQuestionChars)

	sections := []string{
		"You are answering questions only from the uploaded content.",
		fmt.Sprintf("Context modality: %s", normalize(modality)),
		fmt.Sprintf("Conversation history:\n%s", strings.TrimSpace(conversationText)),
		fmt.Sprintf("Retrieved context:\n%s", strings.TrimSpace(contextText)),
		fmt.Sprintf("User question:\n%s", strings.TrimSpace(question)),
		"Instructions:\n" + instructions(modality),
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func instructions(modality string) string {
	base := []string{
		"- Answer using only the retrieved context.",
		"- If the answer is not clearly present, say that it is not available in the uploaded content.",
		"- Be concise but complete.",
		"- When possible, quote exact values from the context.",
	}

	var extra []string
	switch normalize(modality) {
	case ModalityAudio:
		extra = []string{
			"- Prefer explicit uploaded audio metadata for filename, file type, duration, song title, writer, artist, singer, or composer questions.",
			"- Timestamp ranges like [12.50s - 18.20s] are transcript positions, not independent total durations.",
			"- Do not infer the full duration of an audio file from a single transcript segment.",
			"- If the user asks for complete lyrics and the context only contains partial excerpts, clearly say the retrieved context is incomplete.",
			"- For questions about what a song is about, synthesize the overall transcript instead of relying on one isolated lyric line.",
		}
	case ModalityPDF:
		extra = []string{
			"- Prefer explicit uploaded file metadata for file name, file type, extension, or document identity questions.",
			"- Do not confuse the uploaded filename with titles or internal chapter names inside the document.",
			"- Use the document structure and headings when they help answer accurately.",
		}
	case ModalityImage:
		extra = []string{
			"- Answer from visible image details, generated captions, summaries, and uploaded image metadata only.",
			"- Do not infer hidden actions, identities, brands, or scene details unless clearly present.",
			"- Prefer retrieved objects, colors, caption, and summary when describing the image.",
		}
	default:
		extra = []string{
			"- Prefer explicit uploaded file metadata for file identity questions.",
			"- If the context contains timestamps, treat them as excerpt positions unless the total duration is explicitly stated.",
		}
	}

	return strings.Join(append(base, extra...), "\n")
}

func normalize(modality string) string {
	switch strings.ToLower(strings.TrimSpace(modality)) {
	case ModalityAudio:
		return ModalityAudio
	case ModalityPDF:
		return ModalityPDF
	case ModalityImage:
		return ModalityImage
	default:
		return ModalityMixed
	}
}

func clamp(text string, limit int) string {
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
