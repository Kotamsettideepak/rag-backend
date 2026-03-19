package api

import (
	"log"
	"net/http"

	"gin-backend/service"

	"github.com/gin-gonic/gin"
)

func ClearContextHandler(c *gin.Context) {
	log.Printf("[context] clear request received: method=%s path=%s", c.Request.Method, c.Request.URL.Path)

	err := service.DefaultManager().ClearContext()
	if err != nil {
		log.Printf("[context] clear request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear saved context: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Saved context cleared successfully",
	})
}
