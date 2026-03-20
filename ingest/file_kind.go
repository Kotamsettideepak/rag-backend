package ingest

import "strings"

const (
	KindPDF   = "pdf"
	KindAudio = "audio"
)

var supportedAudioExtensions = []string{
	".mp3",
	".wav",
	".m4a",
	".aac",
	".ogg",
	".flac",
	".webm",
	".mp4",
}

func detectKind(filename string, contentType string) string {
	lowerName := strings.ToLower(strings.TrimSpace(filename))
	lowerType := strings.ToLower(strings.TrimSpace(contentType))

	switch {
	case strings.HasSuffix(lowerName, ".pdf"), strings.Contains(lowerType, "pdf"):
		return KindPDF
	case isAudioFile(lowerName, lowerType):
		return KindAudio
	default:
		return "unknown"
	}
}

func isSupportedKind(kind string) bool {
	switch kind {
	case KindPDF, KindAudio:
		return true
	default:
		return false
	}
}

func isAudioFile(filename string, contentType string) bool {
	if strings.HasPrefix(contentType, "audio/") {
		return true
	}

	for _, extension := range supportedAudioExtensions {
		if strings.HasSuffix(filename, extension) {
			return true
		}
	}

	return false
}
