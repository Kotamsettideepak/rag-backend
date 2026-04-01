package chat

import (
	"context"
	"strings"
	"time"

	"gin-backend/client/groq"
	"gin-backend/service/chat/prompt"
)

type retrievalQuery struct {
	Original   string
	Effective  string
	WasRewrite bool
}

func prepareRetrievalQuery(ctx context.Context, question string, history []prompt.HistoryMessage, topicName string) retrievalQuery {
	original := strings.TrimSpace(question)
	query := retrievalQuery{
		Original:  original,
		Effective: original,
	}
	if !shouldRewriteRetrievalQuery(original, history) {
		return query
	}

	rewritten, err := rewriteRetrievalQuery(ctx, original, history, topicName)
	if err != nil {
		return query
	}
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" {
		return query
	}

	query.Effective = rewritten
	query.WasRewrite = true
	return query
}

func shouldRewriteRetrievalQuery(question string, history []prompt.HistoryMessage) bool {
	question = strings.TrimSpace(strings.ToLower(question))
	if question == "" || len(history) == 0 {
		return false
	}

	wordCount := len(strings.Fields(question))
	if wordCount <= 3 {
		return true
	}

	referenceSignals := []string{
		" it ", " this ", " that ", " they ", " them ", " these ", " those ",
		"which one", "second one", "first one", "that one", "this one",
		"explain more", "tell me more", "why is that", "how about", "what about",
		"compare them", "difference between them", "more about it",
	}
	padded := " " + question + " "
	for _, signal := range referenceSignals {
		if strings.Contains(padded, signal) {
			return true
		}
	}

	standaloneHints := []string{
		"what is", "explain", "compare", "difference between", "how does", "why does",
		"java", "thread", "process", "photosynthesis", "calvin", "cycle",
	}
	for _, hint := range standaloneHints {
		if strings.Contains(question, hint) {
			return false
		}
	}

	return wordCount <= 6
}

func rewriteRetrievalQuery(ctx context.Context, question string, history []prompt.HistoryMessage, topicName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rewritePrompt := prompt.BuildRetrievalRewrite(history, question, topicName)
	return groqClient().GenerateResponse([]groq.Message{{Role: "user", Content: rewritePrompt}})
}
