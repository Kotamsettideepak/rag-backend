package chat

import (
	"encoding/json"
	"log"
)

type questionTraceLog struct {
	Question         string `json:"question"`
	FetchedContext   string `json:"fetched_context"`
	ContextSentToLLM string `json:"context_sent_to_llm"`
	ResponseFromLLM  string `json:"response_from_llm"`
}

func logQuestionTrace(question, fetchedContext, contextSentToLLM, responseFromLLM string) {
	payload := questionTraceLog{
		Question:         question,
		FetchedContext:   fetchedContext,
		ContextSentToLLM: contextSentToLLM,
		ResponseFromLLM:  responseFromLLM,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[chat-trace] failed to marshal trace log: %v", err)
		return
	}
	log.Printf("[chat-trace] %s", string(raw))
}

