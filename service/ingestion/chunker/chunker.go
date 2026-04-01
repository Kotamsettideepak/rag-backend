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
	targetSize, overlapSize := c.profileForDocument(doc)

	if (doc.FileKind == "audio" || doc.FileKind == "video") && len(doc.AudioChunks) > 0 {
		chunks, index = c.chunkAudio(doc, index)
		if len(chunks) > 0 {
			return chunks
		}
	}

	chunks, index = c.chunkPageTexts(doc, chunks, index, targetSize, overlapSize)
	if len(chunks) > 0 {
		log.Printf("[chunker] file=%s kind=%s pages=%d profile_target=%d profile_overlap=%d chunks=%d", doc.FileName, doc.FileKind, len(doc.PageTexts), targetSize, overlapSize, len(chunks))
		return chunks
	}

	chunks = c.chunkFlatText(doc, chunks, index, targetSize, overlapSize)
	log.Printf("[chunker] file=%s kind=%s pages=%d profile_target=%d profile_overlap=%d chunks=%d", doc.FileName, doc.FileKind, len(doc.PageTexts), targetSize, overlapSize, len(chunks))
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
				ChatID: doc.ChatID, UserID: doc.UserID, TopicID: doc.TopicID, Page: 1, Index: index,
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
			ChatID: doc.ChatID, UserID: doc.UserID, TopicID: doc.TopicID, Page: index + 2, Index: index,
			Text: text, Hash: hex.EncodeToString(h[:]),
			Metadata: map[string]interface{}{"content_type": transcriptType, "segment_start": ac.Start, "segment_end": ac.End},
		})
		index++
	}
	return chunks, index
}

func (c *Chunker) chunkPageTexts(doc model.ParsedDocument, chunks []model.Chunk, index int, targetSize, overlapSize int) ([]model.Chunk, int) {
	for pageIdx, pageText := range doc.PageTexts {
		for _, text := range c.splitText(pageText, targetSize, overlapSize) {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			h := sha256.Sum256([]byte(text))
			chunks = append(chunks, model.Chunk{
				ID:     doc.FileID + "-" + shortHash(h[:]) + "-" + itoa(index),
				FileID: doc.FileID, FileName: doc.FileName, FileKind: doc.FileKind,
				ChatID: doc.ChatID, UserID: doc.UserID, TopicID: doc.TopicID, Page: pageIdx + 1, Index: index,
				Text: text, Hash: hex.EncodeToString(h[:]),
			})
			index++
		}
	}
	return chunks, index
}

func (c *Chunker) chunkFlatText(doc model.ParsedDocument, chunks []model.Chunk, index int, targetSize, overlapSize int) []model.Chunk {
	for _, text := range c.splitText(doc.Text, targetSize, overlapSize) {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		h := sha256.Sum256([]byte(text))
		chunks = append(chunks, model.Chunk{
			ID:     doc.FileID + "-" + shortHash(h[:]) + "-" + itoa(index),
			FileID: doc.FileID, FileName: doc.FileName, FileKind: doc.FileKind,
			ChatID: doc.ChatID, UserID: doc.UserID, TopicID: doc.TopicID, Page: 1, Index: index,
			Text: text, Hash: hex.EncodeToString(h[:]),
		})
		index++
	}
	return chunks
}

func (c *Chunker) splitText(text string, targetSize, overlapSize int) []string {
	text = normalizeText(text)
	if text == "" {
		return nil
	}
	targetSize, overlapSize = sanitizeProfile(targetSize, overlapSize, c.TargetSize, c.OverlapSize)
	runes := []rune(text)
	if len(runes) <= targetSize {
		return []string{text}
	}
	step := targetSize - overlapSize
	if step <= 0 {
		step = targetSize
	}
	var chunks []string
	for start := 0; start < len(runes); start += step {
		end := start + targetSize
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

func (c *Chunker) profileForDocument(doc model.ParsedDocument) (int, int) {
	targetSize := c.TargetSize
	overlapSize := c.OverlapSize

	// Dynamic PDF profile tuned for small/medium/large page ranges.
	if strings.EqualFold(strings.TrimSpace(doc.FileKind), "pdf") {
		pages := len(doc.PageTexts)
		switch {
		case pages > 0 && pages <= 20:
			targetSize = 700
			overlapSize = 100
		case pages > 20 && pages <= 100:
			targetSize = 1000
			overlapSize = 150
		case pages > 100:
			targetSize = 1400
			overlapSize = 200
		default:
			// Keep env-configured defaults when page information is unavailable.
		}
	}

	return sanitizeProfile(targetSize, overlapSize, c.TargetSize, c.OverlapSize)
}

func sanitizeProfile(targetSize, overlapSize, fallbackTarget, fallbackOverlap int) (int, int) {
	if targetSize <= 0 {
		targetSize = fallbackTarget
	}
	if targetSize <= 0 {
		targetSize = 1000
	}
	if overlapSize < 0 {
		overlapSize = 0
	}
	if overlapSize >= targetSize {
		overlapSize = targetSize / 5
	}
	if overlapSize <= 0 && fallbackOverlap > 0 && fallbackOverlap < targetSize {
		overlapSize = fallbackOverlap
	}
	if overlapSize >= targetSize {
		overlapSize = maxInt(1, targetSize/5)
	}
	return targetSize, overlapSize
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
