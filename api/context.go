package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ClearContextHandler(c *gin.Context) {
	log.Printf("[context] clear request received: method=%s path=%s", c.Request.Method, c.Request.URL.Path)
	c.JSON(http.StatusGone, gin.H{
		"error": "global context clearing is disabled in multi-chat mode",
	})
}
