package api

import (
	"log"
	"net/http"
	"strings"

	"gin-backend/models"
	"gin-backend/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

func UploadHandler(c *gin.Context) {
	log.Printf("[upload] request received: method=%s path=%s", c.Request.Method, c.Request.URL.Path)

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "files are required"})
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "files are required"})
		return
	}

	job, err := service.DefaultManager().SubmitUpload(files)
	if err != nil {
		log.Printf("[upload] failed to enqueue ingestion job: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":   job.ID,
		"status":   job.Status,
		"message":  "Upload accepted. Processing in background.",
		"summary":  "Upload accepted. Processing in background.",
		"files":    job.Files,
		"metrics":  job.Metrics,
		"accepted": true,
	})
}

func StatusHandler(c *gin.Context) {
	jobID := c.Param("job_id")
	job, ok := service.DefaultManager().GetJob(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, statusForClient(job))
}

func statusForClient(job *models.UploadJob) gin.H {
	return gin.H{
		"job_id":       job.ID,
		"status":       job.Status,
		"created_at":   job.CreatedAt,
		"updated_at":   job.UpdatedAt,
		"started_at":   job.StartedAt,
		"completed_at": job.CompletedAt,
		"file_count":   job.FileCount,
		"files":        job.Files,
		"summary":      job.Summary,
		"error":        job.Error,
		"metrics":      job.Metrics,
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

		updates, unsubscribe, err := service.DefaultManager().SubscribeJob(jobID)
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
