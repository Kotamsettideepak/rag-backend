package chat

import (
	"context"
	"strings"
	"time"

	"gin-backend/client/groq"
	"gin-backend/service/chat/prompt"
	"gin-backend/service/ingestion"
)

func (s *Service) VoiceAnswer(
	ctx context.Context,
	userID, chatID, fileName, contentType string,
	payload []byte,
) (VoiceResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	stt, err := deepgramClient().Transcribe(ctx, fileName, payload, contentType)
	if err != nil {
		return VoiceResult{}, err
	}
	question := strings.TrimSpace(stt.Text)
	if question == "" {
		return VoiceResult{}, ErrEmptyTranscript
	}

	conversation := ""
	if strings.TrimSpace(chatID) != "" {
		if _, err := s.chats.Get(ctx, chatID, userID); err != nil {
			return VoiceResult{}, err
		}
		if _, err := s.messages.Save(ctx, chatID, "user", question); err != nil {
			return VoiceResult{}, err
		}
		msgs, err := s.messages.List(ctx, chatID, prompt.RecentContextMessages)
		if err != nil {
			return VoiceResult{}, err
		}
		conversation = BuildConversation(msgs)
	}

	result, err := ingestion.DefaultManager().SearchContext(ctx, question, chatID, userID)
	if err != nil {
		return VoiceResult{}, err
	}

	p := prompt.Build(result.Modality, result.Context, conversation, question)
	answer, err := groqClient().GenerateResponse([]groq.Message{{Role: "user", Content: p}})
	if err != nil {
		return VoiceResult{}, err
	}
	if strings.TrimSpace(chatID) != "" {
		if _, err := s.messages.Save(ctx, chatID, "assistant", answer); err != nil {
			return VoiceResult{}, err
		}
	}

	tts, err := deepgramClient().Synthesize(ctx, answer)
	if err != nil {
		return VoiceResult{}, err
	}
	return VoiceResult{
		Transcript:    question,
		Answer:        answer,
		AudioBase64:   tts.AudioBase64,
		AudioMimeType: tts.MimeType,
	}, nil
}
