package video

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	grqaudio "gin-backend/client/groq"
	"gin-backend/model"
)

const (
	outputBitrateKbps = "64k"
	outputSampleRate  = "16000"
	outputChannels    = "1"
	maxVideoDuration  = 3600.0
)

type Client interface {
	Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error)
}

type HTTPClient struct {
	audioClient grqaudio.AudioClient
}

func NewClient() Client {
	return &HTTPClient{
		audioClient: grqaudio.NewAudioClient(),
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error) {
	if staged.Size > 300*1024*1024 {
		return model.ParsedDocument{}, fmt.Errorf("upload video less than 300 MB")
	}

	duration, err := probeMediaDuration(ctx, staged.StoredPath)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	if duration > maxVideoDuration {
		return model.ParsedDocument{}, fmt.Errorf("upload video less than 1 hour")
	}

	audioPath, audioSize, err := convertVideoToAudio(ctx, staged)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	defer func() {
		_ = os.Remove(audioPath)
	}()

	audioStaged := staged
	audioStaged.StoredPath = audioPath
	audioStaged.Size = audioSize
	audioStaged.DetectedKind = "audio"
	audioStaged.ContentType = "audio/mpeg"

	document, err := c.audioClient.Extract(ctx, audioStaged)
	if err != nil {
		return model.ParsedDocument{}, err
	}

	document.FileName = staged.OriginalName
	document.FileKind = "audio"
	document.FileID = staged.FileID
	document.ChatID = staged.ChatID
	document.UserID = staged.UserID
	return document, nil
}

func convertVideoToAudio(ctx context.Context, staged model.StagedFile) (string, int64, error) {
	if strings.TrimSpace(staged.StoredPath) == "" {
		return "", 0, fmt.Errorf("video file path is required")
	}

	fmt.Printf("[video] conversion started file=%s\n", staged.OriginalName)

	tempFile, err := os.CreateTemp(filepath.Dir(staged.StoredPath), "video-audio-*.mp3")
	if err != nil {
		return "", 0, err
	}
	outputPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return "", 0, err
	}

	command := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-y",
		"-i", staged.StoredPath,
		"-vn",
		"-map", "a:0",
		"-ac", outputChannels,
		"-ar", outputSampleRate,
		"-c:a", "libmp3lame",
		"-b:a", outputBitrateKbps,
		"-threads", "0",
		outputPath,
	)
	startedAt := time.Now()
	output, err := command.CombinedOutput()
	if err != nil {
		_ = os.Remove(outputPath)
		return "", 0, fmt.Errorf("ffmpeg video-to-audio conversion failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		_ = os.Remove(outputPath)
		return "", 0, err
	}
	if info.Size() <= 0 {
		_ = os.Remove(outputPath)
		return "", 0, fmt.Errorf("ffmpeg video-to-audio conversion produced an empty file")
	}

	elapsed := time.Since(startedAt)
	fmt.Printf(
		"[video] conversion completed file=%s took=%.2fs source_bytes=%d audio_bytes=%d\n",
		staged.OriginalName,
		elapsed.Seconds(),
		staged.Size,
		info.Size(),
	)
	return outputPath, info.Size(), nil
}

func probeMediaDuration(ctx context.Context, path string) (float64, error) {
	command := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nokey=1:noprint_wrappers=1",
		path,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration probe failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	durationText := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationText, 64)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid ffprobe duration output: %s", durationText)
	}

	return duration, nil
}
