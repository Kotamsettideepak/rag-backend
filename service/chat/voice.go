package chat

import (
	"context"
	"strings"
	"time"
)

func (s *Service) VoiceAnswer(
	ctx context.Context,
	_, _, fileName, contentType string,
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
	return VoiceResult{
		Transcript: question,
	}, nil
}
