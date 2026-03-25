package chat

import (
	"context"
	"time"

	"gin-backend/client/groq"
	"gin-backend/service/chat/prompt"
	"gin-backend/service/ingestion"
)

func (s *Service) StreamAnswer(
	ctx context.Context,
	userID, chatID, question string,
	onChunk func(string) error,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	if _, err := s.chats.Get(ctx, chatID, userID); err != nil {
		return "", err
	}
	if _, err := s.messages.Save(ctx, chatID, "user", question); err != nil {
		return "", err
	}

	result, err := ingestion.DefaultManager().SearchContext(ctx, question, chatID, userID)
	if err != nil {
		return "", err
	}
	msgs, err := s.messages.List(ctx, chatID, prompt.RecentContextMessages)
	if err != nil {
		return "", err
	}

	p := prompt.Build(result.Modality, result.Context, BuildConversation(msgs), question)
	stream := make(chan string)
	done := make(chan error, 1)
	go func() {
		done <- groqClient().StreamResponse([]groq.Message{{Role: "user", Content: p}}, stream)
	}()

	answer, err := collectStream(ctx, stream, done, onChunk)
	if err != nil {
		return "", err
	}
	if _, err := s.messages.Save(ctx, chatID, "assistant", answer); err != nil {
		return "", err
	}
	return answer, nil
}
