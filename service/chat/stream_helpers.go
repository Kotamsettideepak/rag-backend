package chat

import (
	"context"
	"strings"
)

func collectStream(
	ctx context.Context,
	stream <-chan string,
	done <-chan error,
	onChunk func(string) error,
) (string, error) {
	var builder strings.Builder
	for {
		select {
		case chunk := <-stream:
			builder.WriteString(chunk)
			if err := onChunk(chunk); err != nil {
				return "", err
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
