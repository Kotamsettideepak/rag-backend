package quiz

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"gin-backend/config"
	"gin-backend/middleware"
	quizservice "gin-backend/service/quiz"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

func StartTopicQuizHandler(c *gin.Context) {
	topicID := strings.TrimSpace(c.Param("topic_id"))
	var req struct {
		Topics []string `json:"topics"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	detail, err := quizservice.Default().StartQuiz(c.Request.Context(), user.ID, topicID, req.Topics)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "at least") || strings.Contains(strings.ToLower(err.Error()), "required") {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func TopicQuizHistoryHandler(c *gin.Context) {
	topicID := strings.TrimSpace(c.Param("topic_id"))
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	sessions, err := quizservice.Default().ListHistory(c.Request.Context(), user.ID, topicID, 25)
	if err != nil {
		log.Printf("[quiz] history failed topic=%s err=%v", topicID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load quiz history"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"quizzes": sessions})
}

func TopicQuizDetailHandler(c *gin.Context) {
	quizID := strings.TrimSpace(c.Param("quiz_id"))
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	includeAnswers := strings.EqualFold(strings.TrimSpace(c.Query("include_answers")), "true")
	detail, err := quizservice.Default().GetQuiz(c.Request.Context(), user.ID, quizID, includeAnswers)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func TopicQuizAnswerHandler(c *gin.Context) {
	quizID := strings.TrimSpace(c.Param("quiz_id"))
	questionID := strings.TrimSpace(c.Param("question_id"))
	var req struct {
		Response       string `json:"response"`
		ResponseMode   string `json:"response_mode"`
		ElapsedSeconds int    `json:"elapsed_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	view, err := quizservice.Default().SubmitAnswer(
		c.Request.Context(),
		user.ID,
		quizID,
		questionID,
		req.Response,
		req.ResponseMode,
		req.ElapsedSeconds,
	)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"question": view})
}

func InternalQuizQuestionsHandler(c *gin.Context) {
	if !authorizeInternal(c) {
		return
	}
	var req quizservice.GeneratedQuestionsCallback
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if err := quizservice.Default().ReceiveGeneratedQuestions(c.Request.Context(), req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": true})
}

func CompleteTopicQuizHandler(c *gin.Context) {
	quizID := strings.TrimSpace(c.Param("quiz_id"))
	user, err := middleware.ResolveUser(c)
	if err != nil {
		middleware.RespondAuthError(c, err)
		return
	}
	detail, err := quizservice.Default().CompleteQuiz(c.Request.Context(), user.ID, quizID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func TopicQuizWSHandler(c *gin.Context) {
	quizID := strings.TrimSpace(c.Param("quiz_id"))
	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		user, err := middleware.ResolveUserFromRequest(conn.Request())
		if err != nil {
			_ = websocket.JSON.Send(conn, gin.H{"type": "error", "message": "auth failed"})
			return
		}
		detail, err := quizservice.Default().GetQuiz(conn.Request().Context(), user.ID, quizID, true)
		if err != nil {
			_ = websocket.JSON.Send(conn, gin.H{"type": "error", "message": err.Error()})
			return
		}
		updates, unsubscribe := quizservice.Default().Subscribe(quizID)
		defer unsubscribe()

		if err := websocket.JSON.Send(conn, gin.H{"type": "snapshot", "session": detail}); err != nil {
			return
		}
		for {
			next, ok := <-updates
			if !ok {
				return
			}
			if next.Session.ID != quizID {
				continue
			}
			if _, err := quizservice.Default().GetQuiz(conn.Request().Context(), user.ID, quizID, true); err != nil {
				_ = websocket.JSON.Send(conn, gin.H{"type": "error", "message": fmt.Sprintf("quiz unavailable: %v", err)})
				return
			}
			if err := websocket.JSON.Send(conn, gin.H{"type": "update", "session": next}); err != nil {
				return
			}
		}
	}).ServeHTTP(c.Writer, c.Request)
}

func authorizeInternal(c *gin.Context) bool {
	expected := config.GetQuizInternalToken()
	if expected == "" {
		return true
	}
	actual := strings.TrimSpace(c.GetHeader("X-Internal-Token"))
	if actual == expected {
		return true
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid internal token"})
	return false
}
