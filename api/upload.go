package api

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gin-backend/ingest"
	"gin-backend/models"
	"gin-backend/store"
	"gin-backend/trace"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

func UploadHandler(c *gin.Context) {
	trace.Start("UPLOAD", c.Request.URL.Path)
	log.Printf("[upload] request received: method=%s path=%s", c.Request.Method, c.Request.URL.Path)

	form, err := c.MultipartForm()
	if err != nil {
		lowerErr := strings.ToLower(err.Error())
		if strings.Contains(lowerErr, "too large") || strings.Contains(lowerErr, "request body too large") {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "upload video less than 300 MB"})
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

	maxUploadSizeBytes := getEnvUploadSizeBytes("MAX_UPLOAD_SIZE_MB", 300)
	for _, file := range files {
		if file.Size > maxUploadSizeBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "upload video less than 300 MB"})
			return
		}
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

func StatusHandler(c *gin.Context) {
	jobID := c.Param("job_id")
	log.Printf("[upload-status] http lookup job=%s", jobID)
	job, ok := ingest.DefaultManager().GetJob(jobID)
	if !ok {
		log.Printf("[upload-status] http miss job=%s", jobID)
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	log.Printf("[upload-status] http hit job=%s status=%s stage=%s", jobID, job.Status, job.Stage)
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
		log.Printf("[upload-ws] subscribe start job=%s remote=%s", jobID, conn.Request().RemoteAddr)

		updates, unsubscribe, err := ingest.DefaultManager().SubscribeJob(jobID)
		if err != nil {
			log.Printf("[upload-ws] subscribe miss job=%s remote=%s err=%v", jobID, conn.Request().RemoteAddr, err)
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
				log.Printf("[upload-ws] subscribe closed job=%s remote=%s reason=request_context_done", jobID, conn.Request().RemoteAddr)
				return
			case job, ok := <-updates:
				if !ok {
					log.Printf("[upload-ws] subscribe closed job=%s remote=%s reason=updates_channel_closed", jobID, conn.Request().RemoteAddr)
					return
				}

				payload := statusForClient(job)
				payload["type"] = "status"
				log.Printf("[upload-ws] send status job=%s status=%s stage=%s remote=%s", jobID, job.Status, job.Stage, conn.Request().RemoteAddr)
				if err := websocket.JSON.Send(conn, payload); err != nil {
					log.Printf("[upload-ws] send failed for job=%s: %v", jobID, err)
					return
				}

				if job.Status == models.JobCompleted || job.Status == models.JobFailed {
					log.Printf("[upload-ws] subscribe finished job=%s final_status=%s", jobID, job.Status)
					return
				}
			}
		}
	}).ServeHTTP(c.Writer, c.Request)
}

func getEnvUploadSizeBytes(key string, fallbackMB int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallbackMB << 20
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return fallbackMB << 20
	}

	return value << 20
}
