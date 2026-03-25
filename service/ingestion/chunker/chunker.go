package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strconv"
	"strings"

	"gin-backend/model"
)

// Chunker splits ParsedDocuments into overlapping text chunks.
type Chunker struct {
	TargetSize  int
	OverlapSize int
}

// New creates a Chunker; fallbacks are applied for invalid values.
func New(targetSize, overlapSize int) *Chunker {
	if targetSize <= 0 {
		targetSize = 3500
	}
	if overlapSize <= 0 || overlapSize >= targetSize {
		overlapSize = 700
	}
	return &Chunker{TargetSize: targetSize, OverlapSize: overlapSize}
}

// ChunkDocument produces retrieval chunks from a ParsedDocument.
func (c *Chunker) ChunkDocument(doc model.ParsedDocument) []model.Chunk {
	var chunks []model.Chunk
	index := 0

	if (doc.FileKind == "audio" || doc.FileKind == "video") && len(doc.AudioChunks) > 0 {
		chunks, index = c.chunkAudio(doc, index)
		if len(chunks) > 0 {
			return chunks
		}
	}

	chunks, index = c.chunkPageTexts(doc, chunks, index)
	if len(chunks) > 0 {
		return chunks
	}

	chunks = c.chunkFlatText(doc, chunks, index)
	log.Printf("[chunker] file=%s kind=%s pages=%d chunks=%d", doc.FileName, doc.FileKind, len(doc.PageTexts), len(chunks))
	return chunks
}

func (c *Chunker) chunkAudio(doc model.ParsedDocument, index int) ([]model.Chunk, int) {
	var chunks []model.Chunk
	transcriptType := "audio_transcript"
	if doc.FileKind == "video" {
		transcriptType = "video_transcript"
	}
	if len(doc.PageTexts) > 0 {
		if meta := strings.TrimSpace(doc.PageTexts[0]); meta != "" {
			h := sha256.Sum256([]byte(meta))
			chunks = append(chunks, model.Chunk{
				ID:     doc.FileID + "-" + shortHash(h[:]) + "-" + itoa(index),
				FileID: doc.FileID, FileName: doc.FileName, FileKind: doc.FileKind,
				ChatID: doc.ChatID, UserID: doc.UserID, Page: 1, Index: index,
				Text: meta, Hash: hex.EncodeToString(h[:]),
			})
			index++
		}
	}
	for _, ac := range doc.AudioChunks {
		text := formatAudioChunk(ac)
		if text == "" {
			continue
		}
		h := sha256.Sum256([]byte(text))
		chunks = append(chunks, model.Chunk{
			ID:     doc.FileID + "-" + shortHash(h[:]) + "-" + itoa(index),
			FileID: doc.FileID, FileName: doc.FileName, FileKind: doc.FileKind,
			ChatID: doc.ChatID, UserID: doc.UserID, Page: index + 2, Index: index,
			Text: text, Hash: hex.EncodeToString(h[:]),
			Metadata: map[string]interface{}{"content_type": transcriptType, "segment_start": ac.Start, "segment_end": ac.End},
		})
		index++
	}
	return chunks, index
}

func (c *Chunker) chunkPageTexts(doc model.ParsedDocument, chunks []model.Chunk, index int) ([]model.Chunk, int) {
	for pageIdx, pageText := range doc.PageTexts {
		for _, text := range c.splitText(pageText) {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			h := sha256.Sum256([]byte(text))
			chunks = append(chunks, model.Chunk{
				ID:     doc.FileID + "-" + shortHash(h[:]) + "-" + itoa(index),
				FileID: doc.FileID, FileName: doc.FileName, FileKind: doc.FileKind,
				ChatID: doc.ChatID, UserID: doc.UserID, Page: pageIdx + 1, Index: index,
				Text: text, Hash: hex.EncodeToString(h[:]),
			})
			index++
		}
	}
	return chunks, index
}

func (c *Chunker) chunkFlatText(doc model.ParsedDocument, chunks []model.Chunk, index int) []model.Chunk {
	for _, text := range c.splitText(doc.Text) {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		h := sha256.Sum256([]byte(text))
		chunks = append(chunks, model.Chunk{
			ID:     doc.FileID + "-" + shortHash(h[:]) + "-" + itoa(index),
			FileID: doc.FileID, FileName: doc.FileName, FileKind: doc.FileKind,
			ChatID: doc.ChatID, UserID: doc.UserID, Page: 1, Index: index,
			Text: text, Hash: hex.EncodeToString(h[:]),
		})
		index++
	}
	return chunks
}

func (c *Chunker) splitText(text string) []string {
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
	var chunks []string
	for start := 0; start < len(runes); start += step {
		end := start + c.TargetSize
		if end > len(runes) {
			end = len(runes)
		}
		sliceEnd := end
		if end < len(runes) {
			if adj := nearestBoundary(runes, end); adj > start {
				sliceEnd = adj
			}
		}
		sliceStart := start
		if start > 0 {
			if adj := nearestBoundaryRev(runes, start); adj < end {
				sliceStart = adj
			}
		}
		if chunk := strings.TrimSpace(string(runes[sliceStart:sliceEnd])); chunk != "" {
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
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		out = append(out, strings.Join(strings.Fields(line), " "))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
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

func nearestBoundaryRev(runes []rune, index int) int {
	for i := index; i > 0 && i > index-80; i-- {
		switch runes[i-1] {
		case ' ', '\n', '.', ',', ';', ':':
			return i
		}
	}
	return index
}

func shortHash(hash []byte) string {
	enc := hex.EncodeToString(hash)
	if len(enc) <= 12 {
		return enc
	}
	return enc[:12]
}

func itoa(v int) string { return strconv.Itoa(v) }

func formatAudioChunk(chunk model.AudioTranscriptChunk) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(chunk.Content)), " ")
	if text == "" {
		return ""
	}
	return "[" + strconv.FormatFloat(chunk.Start, 'f', 2, 64) + " - " + strconv.FormatFloat(chunk.End, 'f', 2, 64) + "] " + text
}
