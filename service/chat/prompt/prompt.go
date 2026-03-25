package prompt

import (
	"fmt"
	"strings"
)

const (
	ModalityPDF   = "pdf"
	ModalityAudio = "audio"
	ModalityVideo = "video"
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

	allInstructions := append(baseInstructions(), modalityInstructions(modality)...)
	allInstructions = append(allInstructions, questionInstructions(modality, question)...)

	sections := []string{
		"You are answering questions only from the uploaded content.",
		fmt.Sprintf("Context modality: %s", normalize(modality)),
		fmt.Sprintf("Conversation history:\n%s", strings.TrimSpace(conversationText)),
		fmt.Sprintf("Retrieved context:\n%s", strings.TrimSpace(contextText)),
		fmt.Sprintf("User question:\n%s", strings.TrimSpace(question)),
		"Instructions:\n" + strings.Join(allInstructions, "\n"),
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func baseInstructions() []string {
	return []string{
		"- Answer using only the retrieved context.",
		"- If the answer is not clearly present, say that it is not available in the uploaded content.",
		"- Be concise but complete and easy for a normal user to understand.",
		"- When possible, quote exact values from the context.",
		"- Start with a direct answer instead of generic filler.",
		"- Write naturally. Do not sound robotic, overly defensive, or repetitive.",
		"- Do not dump disconnected transcript fragments as separate points if you can synthesize them into a clearer explanation.",
		"- Do not say 'the actual uploaded context' or similar meta phrasing. Speak directly about the uploaded file.",
	}
}

func modalityInstructions(modality string) []string {
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
	case ModalityVideo:
		extra = []string{
			"- The uploaded content is a video, even if the retrieved context comes from its extracted audio transcript.",
			"- When the user asks what the video is about, summarize the video's spoken content and refer to it as a video.",
			"- Prefer explicit uploaded video metadata for filename, file type, and duration questions.",
			"- Timestamp ranges are transcript positions from the video's extracted audio, not proof that the upload is only audio.",
			"- Do not say the upload is an audio file unless the metadata explicitly says so.",
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

	return extra
}

func questionInstructions(modality, question string) []string {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return nil
	}

	if asksForSummary(q) {
		out := []string{
			"- Give a smooth, user-friendly summary in plain English.",
			"- Lead with the main topic or central message in the first sentence.",
			"- Then explain the important points in a logical flow instead of listing random transcript lines.",
		}
		if normalize(modality) == ModalityVideo {
			out = append(out,
				"- For a video summary, explain what the video mainly discusses, then mention the major themes or segments covered.",
				"- If the transcript is partial or fragmented, still provide the clearest overall summary you can from the available content before noting any limitation.",
			)
		}
		return out
	}

	return nil
}

func asksForSummary(question string) bool {
	for _, phrase := range []string{
		"summarize",
		"summary",
		"what is this about",
		"what's this about",
		"what is the video about",
		"what's the video about",
		"what is this video about",
		"what's this video about",
		"explain the video",
		"tell me about the video",
	} {
		if strings.Contains(question, phrase) {
			return true
		}
	}
	return false
}

func normalize(modality string) string {
	switch strings.ToLower(strings.TrimSpace(modality)) {
	case ModalityAudio:
		return ModalityAudio
	case ModalityVideo:
		return ModalityVideo
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
