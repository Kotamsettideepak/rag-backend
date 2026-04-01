package chat

import (
	"gin-backend/client/deepgram"
	"gin-backend/client/groq"
	"gin-backend/repository/chat"
	"gin-backend/repository/message"
	topicrepo "gin-backend/repository/topic"
	"gin-backend/repository/upload"
)

type Service struct {
	chats    *chat.Repository
	messages *message.Repository
	uploads  *upload.Repository
	topics   *topicrepo.Repository
}

type VoiceResult struct {
	Transcript string
}

func NewService() *Service {
	return &Service{
		chats:    chat.Default(),
		messages: message.Default(),
		uploads:  upload.Default(),
		topics:   topicrepo.Default(),
	}
}

func Default() *Service {
	return NewService()
}

func groqClient() groq.LLMClient {
	return groq.NewClient()
}

func deepgramClient() *deepgram.Client {
	return deepgram.NewClient()
}
