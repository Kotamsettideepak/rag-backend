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
	r.GET("/ws/status/:job_id", api.UploadStatusWebSocketHandler)
	r.POST("/chat/create", api.CreateChatHandler)
	r.GET("/chat/list", api.ListChatsHandler)
	r.GET("/chat/:chat_id/messages", api.ChatMessagesHandler)
	r.POST("/chat", api.ChatHandler)
	r.POST("/voice/chat", api.VoiceChatHandler)
	r.GET("/ws", api.ChatWebSocketHandler)
	r.DELETE("/context", api.ClearContextHandler)
}
