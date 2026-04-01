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

	msgs, err := s.messages.List(ctx, chatID, prompt.RecentContextMessages)
	if err != nil {
		return "", err
	}
	history := BuildPromptHistory(msgs, question)
	retrieval := prepareRetrievalQuery(ctx, question, history, "")

	result, err := ingestion.DefaultManager().SearchContext(ctx, retrieval.Effective, chatID, userID)
	if err != nil {
		return "", err
	}

	p := prompt.Build(result.Modality, history, result.Context, question)
	stream := make(chan string)
	done := make(chan error, 1)
	go func() {
		done <- groqClient().StreamResponse([]groq.Message{{Role: "user", Content: p}}, stream)
	}()

	answer, err := collectStream(ctx, stream, done, onChunk)
	if err != nil {
		return "", err
	}
	logQuestionTrace(question, result.FinalK, result.Context, p, answer)
	if _, err := s.messages.Save(ctx, chatID, "assistant", answer); err != nil {
		return "", err
	}
	return answer, nil
}

func (s *Service) StreamTopicAnswer(
	ctx context.Context,
	topicID, question string,
	history []prompt.HistoryMessage,
	onChunk func(string) error,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	topic, err := s.topics.Get(ctx, topicID)
	if err != nil {
		return "", err
	}

	retrieval := prepareRetrievalQuery(ctx, question, history, topic.Name)
	result, err := ingestion.DefaultManager().SearchTopicContext(ctx, retrieval.Effective, topicID)
	if err != nil {
		return "", err
	}

	p := prompt.BuildTopic(topic.Name, result.Modality, history, result.Context, question)
	stream := make(chan string)
	done := make(chan error, 1)
	go func() {
		done <- groqClient().StreamResponse([]groq.Message{{Role: "user", Content: p}}, stream)
	}()

	answer, err := collectStream(ctx, stream, done, onChunk)
	if err != nil {
		return "", err
	}
	logQuestionTrace(question, result.FinalK, result.Context, p, answer)
	return answer, nil
}
