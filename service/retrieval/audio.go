package retrieval

import (
	"gin-backend/model"
	"gin-backend/repository/vector"
)

type audioIntent int

const (
	intentSemantic audioIntent = iota
	intentMetadata
	intentLyrics
	intentSummary
)

func buildAudioResult(question string, matches []model.SearchMatch, store *vector.Repository) model.SearchContextResult {
	audio := hydrateAudio(filterByKind(matches, ModalityAudio), store)
	switch classifyAudioQuery(question) {
	case intentMetadata:
		return model.SearchContextResult{Context: audioMetadataContext(audio), Modality: ModalityAudio}
	case intentLyrics:
		return model.SearchContextResult{Context: orderedTranscriptContext(audio), Modality: ModalityAudio}
	case intentSummary:
		return model.SearchContextResult{Context: audioSummaryContext(audio), Modality: ModalityAudio}
	default:
		return model.SearchContextResult{Context: audioSemanticContext(audio, matches), Modality: ModalityAudio}
	}
}

func buildVideoResult(question string, matches []model.SearchMatch, store *vector.Repository) model.SearchContextResult {
	video := hydrateByKind(filterByKind(matches, ModalityVideo), store, ModalityVideo)
	switch classifyMediaQuery(question) {
	case intentMetadata:
		return model.SearchContextResult{Context: mediaMetadataContext(video, "video_metadata"), Modality: ModalityVideo}
	case intentSummary:
		return model.SearchContextResult{Context: videoSummaryContext(video), Modality: ModalityVideo}
	default:
		return model.SearchContextResult{Context: videoSemanticContext(video, matches), Modality: ModalityVideo}
	}
}

func audioMetadataContext(matches []model.SearchMatch) string {
	return mediaMetadataContext(matches, "audio_metadata")
}

func orderedTranscriptContext(matches []model.SearchMatch) string {
	transcripts := filterByContentType(matches, "audio_transcript")
	if len(transcripts) == 0 {
		return joinDocs(matches)
	}
	sortAudio(transcripts)
	return joinDocs(dedup(transcripts))
}

func audioSummaryContext(matches []model.SearchMatch) string {
	meta := filterByContentType(matches, "audio_metadata")
	transcripts := filterByContentType(matches, "audio_transcript")
	if len(transcripts) == 0 {
		return joinDocs(matches)
	}
	sortAudio(transcripts)
	parts := make([]string, 0, len(transcripts)+2)
	if len(meta) > 0 {
		parts = append(parts, meta[0].Document)
	}
	parts = append(parts, "Use the full ordered transcript to answer high-level questions about the song.")
	for _, match := range dedup(transcripts) {
		parts = append(parts, match.Document)
	}
	return joinParts(parts)
}

func videoSummaryContext(matches []model.SearchMatch) string {
	transcripts := filterByContentType(matches, "video_transcript")
	if len(transcripts) == 0 {
		return joinDocs(matches)
	}
	sortAudio(transcripts)
	parts := make([]string, 0, len(transcripts)+1)
	parts = append(parts, "The uploaded file is a video. Summarize the video based on its transcript content, not its technical file metadata.")
	for _, match := range dedup(transcripts) {
		parts = append(parts, match.Document)
	}
	return joinParts(parts)
}

func audioSemanticContext(audio, original []model.SearchMatch) string {
	meta := filterByContentType(audio, "audio_metadata")
	transcripts := filterByContentType(audio, "audio_transcript")
	sortAudio(transcripts)
	parts := make([]string, 0, len(meta)+len(transcripts))
	if len(meta) > 0 {
		parts = append(parts, meta[0].Document)
	}
	for _, match := range dedup(transcripts) {
		parts = append(parts, match.Document)
	}
	if len(parts) == 0 {
		return joinDocs(original)
	}
	return joinParts(parts)
}

func videoSemanticContext(video, original []model.SearchMatch) string {
	meta := filterByContentType(video, "video_metadata")
	transcripts := filterByContentType(video, "video_transcript")
	sortAudio(transcripts)
	parts := make([]string, 0, len(meta)+len(transcripts))
	if len(meta) > 0 {
		parts = append(parts, meta[0].Document)
	}
	for _, match := range dedup(transcripts) {
		parts = append(parts, match.Document)
	}
	if len(parts) == 0 {
		return joinDocs(original)
	}
	return joinParts(parts)
}

func hydrateAudio(matches []model.SearchMatch, store *vector.Repository) []model.SearchMatch {
	return hydrateByKind(matches, store, ModalityAudio)
}

func hydrateByKind(matches []model.SearchMatch, store *vector.Repository, kind string) []model.SearchMatch {
	if len(matches) == 0 || store == nil {
		return matches
	}
	fileID := metaStr(matches[0].Metadata, "file_id")
	if fileID == "" {
		return matches
	}
	full, err := store.GetByMetadata(map[string]interface{}{"file_id": fileID}, 512)
	if err != nil || len(full) == 0 {
		return matches
	}
	return filterByKind(full, kind)
}

func mediaMetadataContext(matches []model.SearchMatch, contentType string) string {
	meta := filterByContentType(matches, contentType)
	if len(meta) == 0 {
		return joinDocs(matches)
	}
	return joinDocs(meta)
}
