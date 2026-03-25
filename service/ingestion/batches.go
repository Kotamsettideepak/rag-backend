package ingestion

import (
	"fmt"

	"gin-backend/model"
)

func splitBatches(chunks []model.Chunk, size int) [][]model.Chunk {
	if size <= 0 {
		size = 1
	}
	batches := make([][]model.Chunk, 0, (len(chunks)/size)+1)
	for i := 0; i < len(chunks); i += size {
		end := i + size
		if end > len(chunks) {
			end = len(chunks)
		}
		batches = append(batches, chunks[i:end])
	}
	return batches
}

func validateDocumentLimits(doc model.ParsedDocument) error {
	if doc.FileKind == "pdf" && len(doc.PageTexts) > maxPDFPages {
		return fmt.Errorf("PDF has %d pages; maximum allowed is %d", len(doc.PageTexts), maxPDFPages)
	}
	return nil
}
