package groq

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"gin-backend/config"
)

func buildAudioWindows(duration float64) []audioWindow {
	if duration <= 0 {
		return nil
	}
	chunkSize := config.GetGroqAudioChunkSizeSeconds()
	overlap := config.GetGroqAudioChunkOverlapSeconds()
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}
	windows := make([]audioWindow, 0, int(duration/step)+1)
	for start := 0.0; start < duration; start += step {
		end := start + chunkSize
		if end > duration {
			end = duration
		}
		if end-start > 0 {
			windows = append(windows, audioWindow{Start: start, End: end})
		}
		if end >= duration {
			break
		}
	}
	return windows
}

func splitAudioChunk(ctx context.Context, inputPath, outputPath string, window audioWindow) error {
	command := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", inputPath, "-ss", formatTimestamp(window.Start), "-to", formatTimestamp(window.End), "-c", "copy", outputPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg chunk split failed for %.2f-%.2f seconds: %w: %s", window.Start, window.End, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func probeAudioDuration(ctx context.Context, path string) (float64, error) {
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=nokey=1:noprint_wrappers=1", path)
	output, err := command.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration probe failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid ffprobe duration output: %s", strings.TrimSpace(string(output)))
	}
	return duration, nil
}
