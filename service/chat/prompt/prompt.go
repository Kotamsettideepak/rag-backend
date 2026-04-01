package prompt

import (
	"fmt"
	"strings"
)

type HistoryMessage struct {
	Role    string
	Content string
}

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
func Build(modality string, previousMessages []HistoryMessage, contextText, question string) string {
	previousMessagesText := clamp(formatPreviousMessages(previousMessages), MaxConversationChars)
	contextText = clamp(contextText, MaxContextChars)
	question = clamp(question, MaxQuestionChars)

	allInstructions := append(baseInstructions(), modalityInstructions(modality)...)
	allInstructions = append(allInstructions, questionInstructions(modality, question)...)
	allInstructions = append(allInstructions, historyInstructions()...)

	sections := []string{
		"You are answering questions from uploaded content and, when needed, the recent conversation.",
		fmt.Sprintf("Context modality: %s", normalize(modality)),
		fmt.Sprintf("Previous conversation messages (reference only):\n%s", previousMessagesText),
		fmt.Sprintf("Current question:\n%s", strings.TrimSpace(question)),
		fmt.Sprintf("Current question retrieved context:\n%s", strings.TrimSpace(contextText)),
		"Instructions:\n" + strings.Join(allInstructions, "\n"),
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func BuildTopic(topicName, modality string, previousMessages []HistoryMessage, contextText, question string) string {
	previousMessagesText := clamp(formatPreviousMessages(previousMessages), MaxConversationChars)
	contextText = clamp(contextText, MaxContextChars)
	question = clamp(question, MaxQuestionChars)
	topicName = strings.TrimSpace(topicName)

	allInstructions := append(topicBaseInstructions(), modalityInstructions(modality)...)
	allInstructions = append(allInstructions, topicQuestionInstructions(question)...)
	allInstructions = append(allInstructions, topicHistoryInstructions()...)

	sections := []string{
		"You are answering questions about a selected topic using retrieved topic context and the recent conversation when helpful.",
		fmt.Sprintf("Selected topic: %s", topicName),
		fmt.Sprintf("Context modality: %s", normalize(modality)),
		fmt.Sprintf("Previous conversation messages:\n%s", previousMessagesText),
		fmt.Sprintf("Current question:\n%s", strings.TrimSpace(question)),
		fmt.Sprintf("Retrieved context for this topic:\n%s", strings.TrimSpace(contextText)),
		"Instructions:\n" + strings.Join(allInstructions, "\n"),
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func BuildRetrievalRewrite(previousMessages []HistoryMessage, question, topicName string) string {
	question = clamp(question, MaxQuestionChars)
	previousMessagesText := clamp(formatPreviousMessages(previousMessages), MaxConversationChars)
	topicName = strings.TrimSpace(topicName)

	sections := []string{
		"You rewrite follow-up questions into standalone retrieval queries.",
	}
	if topicName != "" {
		sections = append(sections, fmt.Sprintf("Selected topic:\n%s", topicName))
	}
	sections = append(sections,
		fmt.Sprintf("Previous conversation:\n%s", previousMessagesText),
		fmt.Sprintf("Current user question:\n%s", question),
		"Instructions:\n- Rewrite the current user question into one clear standalone retrieval query.\n- Use previous conversation only to resolve references like it, this, that, they, first one, second one, compare them, explain more, why, or how.\n- Preserve the user's intent.\n- Keep the query concise and retrieval-friendly.\n- Do not answer the question.\n- Do not include explanations, bullet points, or formatting.\n- Output only the rewritten standalone query.",
	)
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func baseInstructions() []string {
	return []string{
		"- Prefer this evidence order: retrieved context, then recent conversation, then minimal general knowledge only when needed for clarity.",
		"- If the answer is not clearly present, say that it is not available in the uploaded content.",
		"- If the user asks for the runtime output of code and the retrieved context does not explicitly include that output, respond formally that code execution is not available here and the output cannot be determined from the provided context.",
		"- Be concise but complete and easy for a normal user to understand.",
		"- When possible, quote exact values from the context.",
		"- Start with a direct answer instead of generic filler.",
		"- Write naturally. Do not sound robotic, overly defensive, or repetitive.",
		"- Do not dump disconnected transcript fragments as separate points if you can synthesize them into a clearer explanation.",
		"- Do not say 'the actual uploaded context' or similar meta phrasing. Speak directly about the uploaded file.",
		"- Do not invent missing details from either the retrieved context or the previous conversation messages.",
		"- If evidence is weak, explicitly say: based on available uploaded content.",
		"- Never fabricate exact page claims, dates, numbers, or citations.",
	}
}

func topicBaseInstructions() []string {
	return []string{
		"- Treat the selected topic as the main subject area for the answer.",
		"- Use the retrieved topic context as the main factual source and use recent conversation only when the question is a follow-up.",
		"- Give the answer directly in clear, natural language.",
		"- Make the answer correct, precise, and easy for a normal user to understand.",
		"- If the user asks a follow-up, use the previous conversation messages to resolve what the user means before answering.",
		"- If the retrieved information is incomplete, give the best supported answer and say briefly what is still uncertain.",
		"- Answer only if the question is actually about the selected topic or clearly supported by the retrieved topic context.",
		"- If the user asks about something outside the selected topic, say briefly that the question is outside this topic and ask them to ask a question related to the selected topic.",
		"- Do not answer unrelated questions using general knowledge just because you know the answer.",
		"- Avoid fabricating exact claims, citations, page numbers, dates, or code output.",
		"- Write in a user-friendly way and avoid unnecessary filler.",
		"- Prefer clarity and correctness over unnecessary brevity.",
		"- Never say phrases like 'according to the retrieved context', 'based on the retrieved context', 'based on the provided document', or 'from the topic context'.",
		"- Do not mention context, retrieval, prompt, document analysis, or source-processing unless the user explicitly asks how you know.",
		"- Do not begin by repeating or rephrasing the user's question.",
		"- Answer as a helpful teacher or assistant, not as a retrieval system.",
		"- Do not narrate your reasoning process before answering.",
		"- Do not force citations or document references unless the user explicitly asks for them.",
	}
}

func historyInstructions() []string {
	return []string{
		"- The previous conversation messages are reference context only.",
		"- Use previous conversation messages only if the current question depends on them, such as follow-up references like this, that, above, previous, second one, or similar wording.",
		"- Use the current question retrieved context as the primary factual source for answers about the uploaded content.",
		"- If the current question is standalone, answer mainly from the current question retrieved context and ignore previous conversation messages unless they are needed for disambiguation.",
		"- If the current question refers to a previous example, code snippet, explanation, or answer, use the previous conversation messages to resolve the reference before answering.",
		"- If previous conversation messages and current retrieved context disagree, prefer the current retrieved context.",
		"- If neither the previous conversation messages nor the current retrieved context clearly support the answer, say the answer is not available.",
	}
}

func topicHistoryInstructions() []string {
	return []string{
		"- Use previous conversation messages when the current question depends on them, especially follow-ups such as this, that, above, previous, second one, why, compare them, or explain more.",
		"- If the current question is standalone, answer mainly from the retrieved topic context.",
		"- If previous conversation messages and current retrieved topic context disagree, prefer the current retrieved topic context.",
		"- If the answer is not fully covered by the retrieved context, you may use minimal topic-relevant background knowledge for coherence, but make sure the final answer stays aligned with the retrieved context.",
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

	intentRules := learningIntentInstructions(q)
	if asksForSummary(q) {
		out := append(intentRules, []string{
			"- Give a smooth, user-friendly summary in plain English.",
			"- Lead with the main subject or central message in the first sentence.",
			"- Then explain the important points in a logical flow instead of listing random transcript lines.",
		}...)
		if normalize(modality) == ModalityVideo {
			out = append(out,
				"- For a video summary, explain what the video mainly discusses, then mention the major themes or segments covered.",
				"- If the transcript is partial or fragmented, still provide the clearest overall summary you can from the available content before noting any limitation.",
			)
		}
		return out
	}

	return intentRules
}

func topicQuestionInstructions(question string) []string {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return nil
	}

	intentRules := learningIntentInstructions(q)
	if asksForSummary(q) {
		return append(intentRules, []string{
			"- Summarize the topic content in a clear, coherent way instead of listing isolated fragments.",
			"- Connect the current question to the selected topic naturally when useful.",
		}...)
	}

	if containsAny(q, "when", "does", "is", "are", "can", "will", "should") {
		intentRules = append(intentRules,
			"- For simple factual questions, start with the answer immediately, then add only the explanation that helps the user.",
		)
	}

	if containsAny(q, "difference between", "compare", "vs", "versus") {
		intentRules = append(intentRules,
			"- For comparison questions, present clear-cut differences point by point.",
			"- Name the compared items explicitly and contrast them on the most relevant criteria.",
			"- Avoid vague comparison wording; make the distinction easy to scan and understand.",
		)
	}

	return intentRules
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

func learningIntentInstructions(question string) []string {
	switch {
	case containsAny(question, "what is", "what's", "define", "meaning of"):
		return []string{
			"- Answer with: one-line definition first, then key points.",
		}
	case containsAny(question, "difference between", "compare", "vs", "versus"):
		return []string{
			"- Answer as a side-by-side comparison with clear criteria and practical differences.",
		}
	case containsAny(question, "when to use", "when should", "best time to use"):
		return []string{
			"- Answer with decision criteria, preconditions, and when not to use.",
		}
	case containsAny(question, "why to use", "why use", "benefits", "advantages", "why should"):
		return []string{
			"- Answer with benefits, trade-offs, and expected outcomes.",
		}
	case containsAny(question, "use cases", "use case", "applications", "where can i use"):
		return []string{
			"- Answer with practical use cases and short real-world examples.",
		}
	default:
		return nil
	}
}

func containsAny(text string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(text, n) {
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

func formatPreviousMessages(messages []HistoryMessage) string {
	if len(messages) == 0 {
		return "No previous messages provided."
	}

	labels := []string{
		"This is last message",
		"This is last second message",
		"This is last third message",
		"This is last fourth message",
		"This is last fifth message",
	}

	lines := make([]string, 0, len(messages))
	for index, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		labelIndex := index
		if labelIndex >= len(labels) {
			labelIndex = len(labels) - 1
		}
		lines = append(lines, fmt.Sprintf("%s\n```text\n%s: %s\n```", labels[labelIndex], strings.ToUpper(strings.TrimSpace(message.Role)), content))
	}
	if len(lines) == 0 {
		return "No previous messages provided."
	}
	return strings.Join(lines, "\n\n")
}
