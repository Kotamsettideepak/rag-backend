package api

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"gin-backend/ingest"
	"gin-backend/models"
	"gin-backend/store"
	"gin-backend/trace"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

type youtubeUploadRequest struct {
	FileKind string `json:"file_kind"`
	URL      string `json:"url"`
	ChatID   string `json:"chat_id"`
}

func UploadHandler(c *gin.Context) {
	trace.Start("UPLOAD", c.Request.URL.Path)
	log.Printf("[upload] request received: method=%s path=%s", c.Request.Method, c.Request.URL.Path)

	if strings.Contains(strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type"))), "application/json") {
		handleYouTubeUpload(c)
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		lowerErr := strings.ToLower(err.Error())
		if strings.Contains(lowerErr, "too large") || strings.Contains(lowerErr, "request body too large") {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "upload is too large; increase MAX_UPLOAD_SIZE_MB or upload a smaller file",
			})
			return
		}

		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to parse uploaded files"})
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "files are required"})
		return
	}

	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}

	chatID := strings.TrimSpace(c.PostForm("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}
	if _, err := store.DefaultStore().GetChat(c.Request.Context(), chatID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}

	for _, file := range files {
		log.Printf(
			"[upload] submitted file name=%s content_type=%s size=%d",
			file.Filename,
			file.Header.Get("Content-Type"),
			file.Size,
		)
	}

	job, err := ingest.DefaultManager().SubmitUpload(files, chatID, user.ID)
	if err != nil {
		log.Printf("[upload] failed to enqueue ingestion job: %v", err)
		trace.End("UPLOAD", "failed to enqueue job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	trace.End("UPLOAD", "accepted job_id="+job.ID)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":           job.ID,
		"status":           job.Status,
		"stage":            job.Stage,
		"message":          "Upload accepted. Processing in background.",
		"summary":          "Upload accepted. Processing in background.",
		"detail":           job.Detail,
		"current_file":     job.CurrentFile,
		"current_kind":     job.CurrentKind,
		"progress_label":   job.ProgressLabel,
		"progress_percent": job.ProgressPercent,
		"files":            job.Files,
		"metrics":          job.Metrics,
		"accepted":         true,
	})
}

func handleYouTubeUpload(c *gin.Context) {
	var req youtubeUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "request body is required"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	req.FileKind = strings.ToLower(strings.TrimSpace(req.FileKind))
	req.URL = strings.TrimSpace(req.URL)
	req.ChatID = strings.TrimSpace(req.ChatID)
	if req.FileKind != "youtube" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_kind must be youtube"})
		return
	}
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}
	if req.ChatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	user, err := resolveCurrentUser(c)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	if _, err := store.DefaultStore().GetChat(c.Request.Context(), req.ChatID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}

	log.Printf("[upload] submitted youtube url=%s kind=%s", req.URL, req.FileKind)

	job, err := ingest.DefaultManager().SubmitYouTube(req.URL, req.ChatID, user.ID)
	if err != nil {
		log.Printf("[upload] failed to enqueue youtube job: %v", err)
		trace.End("UPLOAD", "failed to enqueue youtube job")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trace.End("UPLOAD", "accepted youtube job_id="+job.ID)
	c.JSON(http.StatusAccepted, gin.H{
		"job_id":           job.ID,
		"status":           job.Status,
		"stage":            job.Stage,
		"message":          "YouTube link accepted. Processing in background.",
		"summary":          "YouTube link accepted. Long videos can take a while to download, transcribe, and index.",
		"detail":           job.Detail,
		"current_file":     job.CurrentFile,
		"current_kind":     job.CurrentKind,
		"progress_label":   job.ProgressLabel,
		"progress_percent": job.ProgressPercent,
		"files":            job.Files,
		"metrics":          job.Metrics,
		"accepted":         true,
	})
}

func StatusHandler(c *gin.Context) {
	jobID := c.Param("job_id")
	job, ok := ingest.DefaultManager().GetJob(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, statusForClient(job))
}

func statusForClient(job *models.UploadJob) gin.H {
	return gin.H{
		"job_id":           job.ID,
		"status":           job.Status,
		"stage":            job.Stage,
		"created_at":       job.CreatedAt,
		"updated_at":       job.UpdatedAt,
		"started_at":       job.StartedAt,
		"completed_at":     job.CompletedAt,
		"file_count":       job.FileCount,
		"files":            job.Files,
		"summary":          job.Summary,
		"detail":           job.Detail,
		"current_file":     job.CurrentFile,
		"current_kind":     job.CurrentKind,
		"progress_label":   job.ProgressLabel,
		"progress_percent": job.ProgressPercent,
		"error":            job.Error,
		"metrics":          job.Metrics,
	}
}

func UploadStatusWebSocketHandler(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("job_id"))
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job_id is required"})
		return
	}

	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()

		updates, unsubscribe, err := ingest.DefaultManager().SubscribeJob(jobID)
		if err != nil {
			_ = websocket.JSON.Send(conn, gin.H{
				"type":    "error",
				"message": "job not found",
			})
			return
		}
		defer unsubscribe()

		for {
			select {
			case <-conn.Request().Context().Done():
				return
			case job, ok := <-updates:
				if !ok {
					return
				}

				payload := statusForClient(job)
				payload["type"] = "status"
				if err := websocket.JSON.Send(conn, payload); err != nil {
					log.Printf("[upload-ws] send failed for job=%s: %v", jobID, err)
					return
				}

				if job.Status == models.JobCompleted || job.Status == models.JobFailed {
					return
				}
			}
		}
	}).ServeHTTP(c.Writer, c.Request)
}
