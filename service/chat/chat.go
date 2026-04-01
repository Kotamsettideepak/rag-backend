package chat

import (
	"context"
	"strings"
	"time"

	"gin-backend/client/groq"
	"gin-backend/repository"
	"gin-backend/service/chat/prompt"
	"gin-backend/service/ingestion"
)

func (s *Service) Answer(ctx context.Context, userID, chatID, question string) (string, error) {
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
	answer, err := groqClient().GenerateResponse([]groq.Message{{Role: "user", Content: p}})
	if err != nil {
		return "", err
	}
	logQuestionTrace(question, result.FinalK, result.Context, p, answer)
	if _, err := s.messages.Save(ctx, chatID, "assistant", answer); err != nil {
		return "", err
	}
	return answer, nil
}

func (s *Service) Create(ctx context.Context, userID, title string) (repository.Chat, error) {
	return s.chats.Create(ctx, userID, title)
}

func (s *Service) ListTopics(ctx context.Context, limit int) ([]repository.Topic, error) {
	return s.topics.List(ctx, limit)
}

func (s *Service) AnswerTopic(ctx context.Context, topicID, question string, history []prompt.HistoryMessage) (string, error) {
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
	answer, err := groqClient().GenerateResponse([]groq.Message{{Role: "user", Content: p}})
	if err != nil {
		return "", err
	}
	logQuestionTrace(question, result.FinalK, result.Context, p, answer)
	return answer, nil
}

func (s *Service) List(ctx context.Context, userID string, limit int) ([]repository.Chat, error) {
	return s.chats.List(ctx, userID, limit)
}

func (s *Service) Messages(ctx context.Context, userID, chatID string, limit int) ([]repository.Message, error) {
	if _, err := s.chats.Get(ctx, chatID, userID); err != nil {
		return nil, err
	}
	return s.messages.List(ctx, chatID, limit)
}

func (s *Service) Uploads(ctx context.Context, userID, chatID string) ([]repository.UserUploadedData, error) {
	if _, err := s.chats.Get(ctx, chatID, userID); err != nil {
		return nil, err
	}
	return s.uploads.List(ctx, chatID)
}

func (s *Service) Delete(ctx context.Context, userID, chatID string) error {
	if _, err := s.chats.Get(ctx, chatID, userID); err != nil {
		return err
	}
	if err := ingestion.DefaultManager().DeleteChatContext(chatID, userID); err != nil {
		return err
	}
	return s.chats.Delete(ctx, chatID, userID)
}

func BuildConversation(messages []repository.Message) string {
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		if content := strings.TrimSpace(msg.Content); content != "" {
			lines = append(lines, strings.ToUpper(msg.Role)+": "+content)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func BuildPromptHistory(messages []repository.Message, currentQuestion string) []prompt.HistoryMessage {
	history := make([]prompt.HistoryMessage, 0, len(messages))
	for index := len(messages) - 1; index >= 0; index-- {
		content := strings.TrimSpace(messages[index].Content)
		if content == "" {
			continue
		}
		if len(history) == 0 && messages[index].Role == "user" && content == strings.TrimSpace(currentQuestion) {
			continue
		}
		history = append(history, prompt.HistoryMessage{
			Role:    messages[index].Role,
			Content: content,
		})
	}
	return history
}
