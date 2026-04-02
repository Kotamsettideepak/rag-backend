package chat

import (
	"context"
	"strings"

	"gin-backend/client/groq"
)

func collectStream(
	ctx context.Context,
	stream <-chan groq.StreamEvent,
	done <-chan error,
	onChunk func(string) error,
	onThinking func(string) error,
) (string, error) {
	var builder strings.Builder
	for {
		select {
		case event := <-stream:
			if event.Thinking != "" {
				if err := onThinking(event.Thinking); err != nil {
					return "", err
				}
			}
			if event.Content != "" {
				builder.WriteString(event.Content)
				if err := onChunk(event.Content); err != nil {
					return "", err
				}
			}
		case err := <-done:
			if err != nil {
				return "", err
			}
			return builder.String(), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
