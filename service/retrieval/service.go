package retrieval

import (
	"gin-backend/model"
	"gin-backend/repository/vector"
)

func BuildContextResult(question string, matches []model.SearchMatch, store *vector.Repository) model.SearchContextResult {
	if len(matches) == 0 {
		return model.SearchContextResult{Context: "", Modality: ModalityMixed}
	}

	audio := filterByKind(matches, ModalityAudio)
	video := filterByKind(matches, ModalityVideo)
	images := filterByKind(matches, ModalityImage)
	pdfs := filterByKind(matches, ModalityPDF)

	switch {
	case len(video) > 0 && len(audio) == 0 && len(images) == 0 && len(pdfs) == 0:
		return buildVideoResult(question, matches, store)
	case len(audio) > 0 && len(images) == 0 && len(pdfs) == 0:
		return buildAudioResult(question, matches, store)
	case len(images) > 0 && len(audio) == 0 && len(pdfs) == 0:
		return model.SearchContextResult{Context: joinDocs(matches), Modality: ModalityImage}
	case len(pdfs) > 0 && len(audio) == 0 && len(images) == 0:
		return model.SearchContextResult{Context: joinDocs(matches), Modality: ModalityPDF}
	default:
		return model.SearchContextResult{Context: joinDocs(matches), Modality: ModalityMixed}
	}
}
