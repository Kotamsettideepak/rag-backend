package routes

import (
	"gin-backend/handler/chat"
	"gin-backend/handler/quiz"
	"gin-backend/handler/topic"
	"gin-backend/handler/upload"
	"gin-backend/handler/voice"

	"github.com/gin-gonic/gin"
)

// Register wires all routes onto the engine.
func Register(r *gin.Engine) {
	r.GET("/", func(c *gin.Context) { c.JSON(200, gin.H{"message": "Server working"}) })
	r.GET("/ping", func(c *gin.Context) { c.JSON(200, gin.H{"message": "pong"}) })

	r.POST("/topic", topic.CreateTopicHandler)
	r.POST("/internal/topic-jobs/start", topic.TopicJobStartHandler)
	r.POST("/internal/topic-jobs/chunks", topic.IngestTopicChunksHandler)
	r.POST("/internal/topic-jobs/chunk-failures", topic.TopicJobChunkFailuresHandler)
	r.POST("/internal/topic-jobs/complete", topic.TopicJobCompleteHandler)

	// Upload
	r.POST("/upload", upload.UploadHandler)
	r.GET("/status/:job_id", upload.StatusHandler)
	r.GET("/ws/status/:job_id", upload.UploadWSHandler)

	// Chat (REST)
	r.POST("/chat/create", chat.CreateChatHandler)
	r.GET("/chat/list", chat.ListChatsHandler)
	r.GET("/topic/list", chat.ListTopicsHandler)
	r.GET("/chat/:chat_id/messages", chat.ChatMessagesHandler)
	r.GET("/chat/:chat_id/uploads", chat.ChatUploadsHandler)
	r.DELETE("/chat/:chat_id", chat.DeleteChatHandler)
	r.POST("/chat", chat.ChatHandler)
	r.DELETE("/context", chat.ClearContextHandler)
	r.POST("/topic/:topic_id/quiz/start", quiz.StartTopicQuizHandler)
	r.GET("/topic/:topic_id/quizzes", quiz.TopicQuizHistoryHandler)
	r.GET("/topic/quiz/:quiz_id", quiz.TopicQuizDetailHandler)
	r.POST("/topic/quiz/:quiz_id/questions/:question_id/answer", quiz.TopicQuizAnswerHandler)
	r.POST("/topic/quiz/:quiz_id/complete", quiz.CompleteTopicQuizHandler)
	r.GET("/ws/topic-quiz/:quiz_id", quiz.TopicQuizWSHandler)
	r.POST("/internal/quiz/jobs/questions", quiz.InternalQuizQuestionsHandler)

	// Chat (WebSocket streaming)
	r.GET("/ws", chat.WebSocketHandler)

	// Voice
	r.POST("/voice/chat", voice.VoiceChatHandler)
}
