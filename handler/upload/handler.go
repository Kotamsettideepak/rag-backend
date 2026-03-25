package upload

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gin-backend/middleware"
	"gin-backend/model"
	chatrepo "gin-backend/repository/chat"
	"gin-backend/service/ingestion"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

// UploadHandler handles POST /upload.
func UploadHandler(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		msg := "failed to parse uploaded files"
		if lower := strings.ToLower(err.Error()); strings.Contains(lower, "too large") {
			msg = "upload exceeds size limit"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "files are required"})
		return
	}

	maxBytes := envSizeBytes("MAX_UPLOAD_SIZE_MB", 300)
	for _, f := range files {
		if f.Size > maxBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "upload exceeds 300 MB limit"})
			return
		}
	}

	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}

	chatID := strings.TrimSpace(c.PostForm("chat_id"))
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}
	if _, err := chatrepo.Default().Get(c.Request.Context(), chatID, user.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}

	job, err := ingestion.DefaultManager().SubmitUpload(files, chatID, user.ID)
	if err != nil {
		log.Printf("[upload] enqueue failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, jobPayload(job))
}

// StatusHandler handles GET /status/:job_id.
func StatusHandler(c *gin.Context) {
	jobID := c.Param("job_id")
	job, ok := ingestion.DefaultManager().GetJob(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	c.JSON(http.StatusOK, jobPayload(job))
}

// UploadWSHandler handles GET /ws/status/:job_id (WebSocket status stream).
func UploadWSHandler(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("job_id"))
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job_id is required"})
		return
	}

	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		log.Printf("[upload-ws] subscribe job=%s remote=%s", jobID, conn.Request().RemoteAddr)

		updates, unsub, err := ingestion.DefaultManager().SubscribeJob(jobID)
		if err != nil {
			_ = websocket.JSON.Send(conn, gin.H{"type": "error", "message": "job not found"})
			return
		}
		defer unsub()

		for {
			select {
			case <-conn.Request().Context().Done():
				return
			case job, ok := <-updates:
				if !ok {
					return
				}
				payload := jobPayload(job)
				payload["type"] = "status"
				if err := websocket.JSON.Send(conn, payload); err != nil {
					log.Printf("[upload-ws] send failed job=%s: %v", jobID, err)
					return
				}
				if job.Status == model.JobCompleted || job.Status == model.JobFailed {
					return
				}
			}
		}
	}).ServeHTTP(c.Writer, c.Request)
}

func jobPayload(job *model.UploadJob) gin.H {
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
		"total_chunks":     job.TotalChunks,
		"completed_chunks": job.CompletedChunks,
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

func envSizeBytes(key string, fallbackMB int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallbackMB << 20
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return fallbackMB << 20
	}
	return v << 20
}
