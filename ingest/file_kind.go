package ingest

import "strings"

const (
	KindPDF   = "pdf"
	KindAudio = "audio"
	KindImage = "image"
	KindVideo = "video"
)

var supportedAudioExtensions = []string{
	".mp3",
	".wav",
	".m4a",
	".aac",
	".ogg",
	".flac",
	".webm",
}

var supportedVideoExtensions = []string{
	".mp4",
	".mov",
	".avi",
	".mkv",
	".m4v",
	".mpeg",
	".mpg",
	".wmv",
	".3gp",
}

var supportedImageExtensions = []string{
	".png",
	".jpg",
	".jpeg",
	".webp",
	".bmp",
	".gif",
}

func detectKind(filename string, contentType string) string {
	lowerName := strings.ToLower(strings.TrimSpace(filename))
	lowerType := strings.ToLower(strings.TrimSpace(contentType))

	switch {
	case strings.HasSuffix(lowerName, ".pdf"), strings.Contains(lowerType, "pdf"):
		return KindPDF
	case isVideoFile(lowerName, lowerType):
		return KindVideo
	case isAudioFile(lowerName, lowerType):
		return KindAudio
	case isImageFile(lowerName, lowerType):
		return KindImage
	default:
		return "unknown"
	}
}

func isSupportedKind(kind string) bool {
	switch kind {
	case KindPDF, KindAudio, KindImage, KindVideo:
		return true
	default:
		return false
	}
}

func isVideoFile(filename string, contentType string) bool {
	if strings.HasPrefix(contentType, "video/") {
		return true
	}

	for _, extension := range supportedVideoExtensions {
		if strings.HasSuffix(filename, extension) {
			return true
		}
	}

	return false
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

func isImageFile(filename string, contentType string) bool {
	if strings.HasPrefix(contentType, "image/") {
		return true
	}

	for _, extension := range supportedImageExtensions {
		if strings.HasSuffix(filename, extension) {
			return true
		}
	}

	return false
}
