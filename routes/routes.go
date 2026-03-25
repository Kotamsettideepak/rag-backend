package routes

import (
	"gin-backend/handler/chat"
	"gin-backend/handler/upload"
	"gin-backend/handler/voice"

	"github.com/gin-gonic/gin"
)

// Register wires all routes onto the engine.
func Register(r *gin.Engine) {
	r.GET("/", func(c *gin.Context) { c.JSON(200, gin.H{"message": "Server working"}) })
	r.GET("/ping", func(c *gin.Context) { c.JSON(200, gin.H{"message": "pong"}) })

	// Upload
	r.POST("/upload", upload.UploadHandler)
	r.GET("/status/:job_id", upload.StatusHandler)
	r.GET("/ws/status/:job_id", upload.UploadWSHandler)

	// Chat (REST)
	r.POST("/chat/create", chat.CreateChatHandler)
	r.GET("/chat/list", chat.ListChatsHandler)
	r.GET("/chat/:chat_id/messages", chat.ChatMessagesHandler)
	r.GET("/chat/:chat_id/uploads", chat.ChatUploadsHandler)
	r.DELETE("/chat/:chat_id", chat.DeleteChatHandler)
	r.POST("/chat", chat.ChatHandler)
	r.DELETE("/context", chat.ClearContextHandler)

	// Chat (WebSocket streaming)
	r.GET("/ws", chat.WebSocketHandler)

	// Voice
	r.POST("/voice/chat", voice.VoiceChatHandler)
}
