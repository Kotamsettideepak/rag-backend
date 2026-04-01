package topic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gin-backend/config"
	"gin-backend/model"
	topicrepo "gin-backend/repository/topic"
	"gin-backend/service/topicingest"

	"github.com/gin-gonic/gin"
)

type createTopicRequest struct {
	TopicName string `json:"topic_name"`
}

type ingestChunksRequest struct {
	TopicID    string             `json:"topic_id"`
	JobID      string             `json:"job_id"`
	FileID     string             `json:"file_id"`
	FileName   string             `json:"file_name"`
	FileKind   string             `json:"file_kind"`
	Status     string             `json:"status"`
	BatchIndex int                `json:"batch_index"`
	Chunks     []ingestChunkInput `json:"chunks"`
}

type ingestChunkInput struct {
	ID             string `json:"id"`
	Text           string `json:"text"`
	Page           int    `json:"page"`
	PageNumber     int    `json:"page_number"`
	Index          int    `json:"index"`
	ChunkIndex     int    `json:"chunk_index"`
	Type           string `json:"type"`
	ChunkType      string `json:"chunk_type"`
	SectionTitle   string `json:"section_title"`
	PageRange      string `json:"page_range"`
	PageFrom       int    `json:"page_from"`
	PageTo         int    `json:"page_to"`
	CodeLanguage   string `json:"code_language"`
	FormulaLatex   string `json:"formula_latex"`
	HasFormula     bool   `json:"has_formula"`
	PictureCaption string `json:"picture_caption"`
	PictureClass   string `json:"picture_class"`
	Topic          string `json:"topic"`
	SubTopic       string `json:"sub_topic"`
	FileID         string `json:"file_id"`
	FileName       string `json:"file_name"`
	FileKind       string `json:"file_kind"`
}

func CreateTopicHandler(c *gin.Context) {
	if !authorizeInternal(c) {
		return
	}

	var req createTopicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	topicID, status, err := topicingest.Default().CreateTopic(c.Request.Context(), strings.TrimSpace(req.TopicName))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"topic_id": topicID, "status": status})
}

func IngestTopicChunksHandler(c *gin.Context) {
	if !authorizeInternal(c) {
		return
	}

	var req ingestChunksRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	chunks, err := buildChunks(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := topicingest.Default().Enqueue(c.Request.Context(), topicingest.Request{
		TopicID: req.TopicID,
		Status:  req.Status,
		Chunks:  chunks,
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"accepted":      true,
		"topic_id":      strings.TrimSpace(req.TopicID),
		"queued_chunks": len(chunks),
		"status":        strings.TrimSpace(req.Status),
	})
}

func TopicJobStartHandler(c *gin.Context) {
	if !authorizeInternal(c) {
		return
	}
	var req struct {
		TopicID string `json:"topic_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if err := topicingest.Default().Enqueue(c.Request.Context(), topicingest.Request{
		TopicID: req.TopicID,
		Status:  "In Progress",
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": true})
}

func TopicJobCompleteHandler(c *gin.Context) {
	handleTopicStatusOnly(c, "Completed")
}

func TopicJobChunkFailuresHandler(c *gin.Context) {
	if !authorizeInternal(c) {
		return
	}
	var req struct {
		JobID        string `json:"job_id"`
		TopicID      string `json:"topic_id"`
		FailedChunks []struct {
			FileID     string `json:"file_id"`
			ChunkIndex int    `json:"chunk_index"`
			Reason     string `json:"reason"`
			Attempts   int    `json:"attempts"`
			Payload    string `json:"payload"`
		} `json:"failed_chunks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	rows := make([]topicrepo.FailedChunk, 0, len(req.FailedChunks))
	for _, item := range req.FailedChunks {
		rows = append(rows, topicrepo.FailedChunk{
			JobID:      req.JobID,
			TopicID:    req.TopicID,
			FileID:     item.FileID,
			ChunkIndex: item.ChunkIndex,
			Reason:     item.Reason,
			Attempts:   item.Attempts,
			Payload:    item.Payload,
		})
	}
	if err := topicrepo.Default().SaveFailures(c.Request.Context(), rows); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": true, "saved_failures": len(rows)})
}

func handleTopicStatusOnly(c *gin.Context, status string) {
	if !authorizeInternal(c) {
		return
	}
	var req struct {
		TopicID string `json:"topic_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if err := topicingest.Default().Enqueue(c.Request.Context(), topicingest.Request{
		TopicID: req.TopicID,
		Status:  status,
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": true})
}

func authorizeInternal(c *gin.Context) bool {
	expected := config.GetTopicIngestInternalToken()
	if expected == "" {
		return true
	}
	actual := strings.TrimSpace(c.GetHeader("X-Internal-Token"))
	if actual == expected {
		return true
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid internal token"})
	return false
}

func buildChunks(req ingestChunksRequest) ([]model.Chunk, error) {
	topicID := strings.TrimSpace(req.TopicID)
	if topicID == "" {
		return nil, fmt.Errorf("topic_id is required")
	}

	if len(req.Chunks) == 0 {
		return nil, nil
	}

	fileID := strings.TrimSpace(req.FileID)
	fileName := strings.TrimSpace(req.FileName)
	fileKind := strings.TrimSpace(req.FileKind)
	if fileKind == "" {
		fileKind = "pdf"
	}

	chunks := make([]model.Chunk, 0, len(req.Chunks))
	for idx, input := range req.Chunks {
		text := strings.TrimSpace(input.Text)
		if text == "" {
			continue
		}
		chunkIndex := firstPositive(input.Index, input.ChunkIndex, idx)
		page := firstPositive(input.Page, input.PageNumber, 1)
		sum := sha256.Sum256([]byte(text))
		resolvedFileID := firstNonEmpty(strings.TrimSpace(input.FileID), fileID, topicID)
		resolvedFileName := firstNonEmpty(strings.TrimSpace(input.FileName), fileName, "topic.pdf")
		resolvedFileKind := firstNonEmpty(strings.TrimSpace(input.FileKind), fileKind, "pdf")

		metadata := map[string]interface{}{
			"topic_id":        topicID,
			"section_title":   strings.TrimSpace(input.SectionTitle),
			"page_range":      strings.TrimSpace(input.PageRange),
			"chunk_type":      strings.TrimSpace(firstNonEmpty(input.ChunkType, input.Type, "text")),
			"code_language":   strings.TrimSpace(input.CodeLanguage),
			"formula_latex":   strings.TrimSpace(input.FormulaLatex),
			"has_formula":     input.HasFormula,
			"picture_class":   strings.TrimSpace(input.PictureClass),
			"picture_caption": strings.TrimSpace(input.PictureCaption),
			"page_from":       input.PageFrom,
			"page_to":         input.PageTo,
			"chunk_idx":       chunkIndex,
			"topic":           strings.TrimSpace(input.Topic),
			"sub_topic":       strings.TrimSpace(input.SubTopic),
		}

		chunkID := strings.TrimSpace(input.ID)
		if chunkID == "" {
			chunkID = resolvedFileID + "-" + shortHash(sum[:]) + "-" + strconv.Itoa(chunkIndex)
		}

		chunks = append(chunks, model.Chunk{
			ID:       chunkID,
			FileID:   resolvedFileID,
			FileName: resolvedFileName,
			FileKind: resolvedFileKind,
			TopicID:  topicID,
			Page:     page,
			Index:    chunkIndex,
			Text:     text,
			Hash:     hex.EncodeToString(sum[:]),
			Metadata: metadata,
		})
	}
	return chunks, nil
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func shortHash(hash []byte) string {
	enc := hex.EncodeToString(hash)
	if len(enc) <= 12 {
		return enc
	}
	return enc[:12]
}
