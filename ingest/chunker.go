package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strconv"
	"strings"

	"gin-backend/models"
)

type Chunker struct {
	TargetSize  int
	OverlapSize int
}

func NewChunker(targetSize int, overlapSize int) *Chunker {
	if targetSize <= 0 {
		targetSize = 3500
	}
	if overlapSize <= 0 || overlapSize >= targetSize {
		overlapSize = 700
	}

	return &Chunker{
		TargetSize:  targetSize,
		OverlapSize: overlapSize,
	}
}

func (c *Chunker) ChunkDocument(doc models.ParsedDocument) []models.Chunk {
	var chunks []models.Chunk
	index := 0

	if doc.FileKind == "audio" && len(doc.AudioChunks) > 0 {
		if len(doc.PageTexts) > 0 {
			metadataText := strings.TrimSpace(doc.PageTexts[0])
			if metadataText != "" {
				hashBytes := sha256.Sum256([]byte(metadataText))
				chunks = append(chunks, models.Chunk{
					ID:       doc.FileID + "-" + hashBytesToShort(hashBytes[:]) + "-" + itoa(index),
					FileID:   doc.FileID,
					FileName: doc.FileName,
					FileKind: doc.FileKind,
					ChatID:   doc.ChatID,
					UserID:   doc.UserID,
					Page:     1,
					Index:    index,
					Text:     metadataText,
					Hash:     hex.EncodeToString(hashBytes[:]),
				})
				index++
			}
		}

		for _, audioChunk := range doc.AudioChunks {
			chunkText := formatAudioChunkText(audioChunk)
			if chunkText == "" {
				continue
			}

			hashBytes := sha256.Sum256([]byte(chunkText))
			chunks = append(chunks, models.Chunk{
				ID:       doc.FileID + "-" + hashBytesToShort(hashBytes[:]) + "-" + itoa(index),
				FileID:   doc.FileID,
				FileName: doc.FileName,
				FileKind: doc.FileKind,
				ChatID:   doc.ChatID,
				UserID:   doc.UserID,
				Page:     index + 2,
				Index:    index,
				Text:     chunkText,
				Hash:     hex.EncodeToString(hashBytes[:]),
				Metadata: map[string]interface{}{
					"content_type":  audioChunk.Type,
					"segment_start": audioChunk.Start,
					"segment_end":   audioChunk.End,
				},
			})
			index++
		}

		if len(chunks) > 0 {
			log.Printf(
				"[chunker] file=%s kind=%s pages=%d produced_chunks=%d target_size=%d overlap=%d first_chunk_preview=%s",
				doc.FileName,
				doc.FileKind,
				len(doc.PageTexts),
				len(chunks),
				c.TargetSize,
				c.OverlapSize,
				firstChunkPreview(chunks),
			)
			return chunks
		}
	}

	for pageIndex, pageText := range doc.PageTexts {
		pageChunks := c.chunkText(pageText)
		for _, chunkText := range pageChunks {
			chunkText = strings.TrimSpace(chunkText)
			if chunkText == "" {
				continue
			}

			hashBytes := sha256.Sum256([]byte(chunkText))
			chunks = append(chunks, models.Chunk{
				ID:       doc.FileID + "-" + hashBytesToShort(hashBytes[:]) + "-" + itoa(index),
				FileID:   doc.FileID,
				FileName: doc.FileName,
				FileKind: doc.FileKind,
				ChatID:   doc.ChatID,
				UserID:   doc.UserID,
				Page:     pageIndex + 1,
				Index:    index,
				Text:     chunkText,
				Hash:     hex.EncodeToString(hashBytes[:]),
			})
			index++
		}
	}

	if len(chunks) > 0 {
		return chunks
	}

	for _, chunkText := range c.chunkText(doc.Text) {
		chunkText = strings.TrimSpace(chunkText)
		if chunkText == "" {
			continue
		}

		hashBytes := sha256.Sum256([]byte(chunkText))
		chunks = append(chunks, models.Chunk{
			ID:       doc.FileID + "-" + hashBytesToShort(hashBytes[:]) + "-" + itoa(index),
			FileID:   doc.FileID,
			FileName: doc.FileName,
			FileKind: doc.FileKind,
			ChatID:   doc.ChatID,
			UserID:   doc.UserID,
			Page:     1,
			Index:    index,
			Text:     chunkText,
			Hash:     hex.EncodeToString(hashBytes[:]),
		})
		index++
	}

	log.Printf(
		"[chunker] file=%s kind=%s pages=%d produced_chunks=%d target_size=%d overlap=%d first_chunk_preview=%s",
		doc.FileName,
		doc.FileKind,
		len(doc.PageTexts),
		len(chunks),
		c.TargetSize,
		c.OverlapSize,
		firstChunkPreview(chunks),
	)

	return chunks
}

func (c *Chunker) chunkText(text string) []string {
	text = normalizeText(text)
	if text == "" {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= c.TargetSize {
		return []string{text}
	}

	step := c.TargetSize - c.OverlapSize
	if step <= 0 {
		step = c.TargetSize
	}

	chunks := make([]string, 0, (len(runes)/step)+1)
	for start := 0; start < len(runes); start += step {
		end := start + c.TargetSize
		if end > len(runes) {
			end = len(runes)
		}

		sliceStart := start
		sliceEnd := end
		if end < len(runes) {
			if adjustedEnd := nearestBoundary(runes, end); adjustedEnd > start {
				sliceEnd = adjustedEnd
			}
		}
		if start > 0 {
			if adjustedStart := nearestBoundaryReverse(runes, start); adjustedStart < end {
				sliceStart = adjustedStart
			}
		}

		chunk := strings.TrimSpace(string(runes[sliceStart:sliceEnd]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if end == len(runes) {
			break
		}
	}

	return chunks
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	normalized := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(normalized) == 0 || normalized[len(normalized)-1] == "" {
				continue
			}
			normalized = append(normalized, "")
			continue
		}

		normalized = append(normalized, strings.Join(strings.Fields(line), " "))
	}

	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

func nearestBoundary(runes []rune, index int) int {
	for i := index; i < len(runes) && i < index+80; i++ {
		switch runes[i] {
		case ' ', '\n', '.', ',', ';', ':':
			return i
		}
	}
	return index
}

func nearestBoundaryReverse(runes []rune, index int) int {
	for i := index; i > 0 && i > index-80; i-- {
		switch runes[i-1] {
		case ' ', '\n', '.', ',', ';', ':':
			return i
		}
	}
	return index
}

func hashBytesToShort(hash []byte) string {
	encoded := hex.EncodeToString(hash)
	if len(encoded) <= 12 {
		return encoded
	}
	return encoded[:12]
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func firstChunkPreview(chunks []models.Chunk) string {
	if len(chunks) == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(strings.TrimSpace(chunks[0].Text)), " ")
	if len(text) <= 180 {
		return text
	}
	return text[:180] + "..."
}

func formatAudioChunkText(chunk models.AudioTranscriptChunk) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(chunk.Content)), " ")
	if text == "" {
		return ""
	}
	return "[" + strconv.FormatFloat(chunk.Start, 'f', 2, 64) + " - " + strconv.FormatFloat(chunk.End, 'f', 2, 64) + "] " + text
}
