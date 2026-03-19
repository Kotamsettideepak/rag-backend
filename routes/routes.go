package routes

import (
	"gin-backend/api"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine) {
	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Server working",
		})
	})

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	r.POST("/upload", api.UploadHandler)
	r.GET("/status/:job_id", api.StatusHandler)
	r.POST("/chat", api.ChatHandler)
	r.DELETE("/context", api.ClearContextHandler)
}
