package chat

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"gin-backend/middleware"
	chatservice "gin-backend/service/chat"
	chatserviceprompt "gin-backend/service/chat/prompt"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

type wsRequest struct {
	Type           string                             `json:"type"`
	ChatID         string                             `json:"chat_id"`
	TopicID        string                             `json:"topic_id"`
	Question       string                             `json:"question"`
	RecentMessages []chatserviceprompt.HistoryMessage `json:"recent_messages"`
}

type wsResponse struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Answer  string `json:"answer,omitempty"`
	Message string `json:"message,omitempty"`
}

// WebSocketHandler handles the GET /ws streaming chat endpoint.
func WebSocketHandler(c *gin.Context) {
	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		conn.PayloadType = websocket.TextFrame

		for {
			var req wsRequest
			if err := websocket.JSON.Receive(conn, &req); err != nil {
				if !isClosedErr(err) {
					log.Printf("[ws] receive error: %v", err)
				}
				return
			}

			chatID := strings.TrimSpace(req.ChatID)
			topicID := strings.TrimSpace(req.TopicID)
			question := strings.TrimSpace(req.Question)
			if question == "" || (chatID == "" && topicID == "") {
				_ = websocket.JSON.Send(conn, wsResponse{Type: "error", Message: "question and one of chat_id or topic_id are required"})
				continue
			}

			if err := streamAnswer(conn, chatID, topicID, question, req.RecentMessages); err != nil {
				log.Printf("[ws] stream error: %v", err)
				_ = websocket.JSON.Send(conn, wsResponse{Type: "error", Message: err.Error()})
			}
		}
	}).ServeHTTP(c.Writer, c.Request)
}

func streamAnswer(conn *websocket.Conn, chatID, topicID, question string, history []chatserviceprompt.HistoryMessage) error {
	user, err := middleware.ResolveUserFromRequest(conn.Request())
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}
	_ = user

	if err := websocket.JSON.Send(conn, wsResponse{Type: "start"}); err != nil {
		return err
	}
	var answer string
	if topicID != "" {
		answer, err = chatservice.Default().StreamTopicAnswer(context.Background(), topicID, question, history, func(chunk string) error {
			return websocket.JSON.Send(conn, wsResponse{Type: "chunk", Content: chunk})
		}, func(thinking string) error {
			return websocket.JSON.Send(conn, wsResponse{Type: "thinking", Thinking: thinking})
		})
	} else {
		answer, err = chatservice.Default().StreamAnswer(context.Background(), user.ID, chatID, question, func(chunk string) error {
			return websocket.JSON.Send(conn, wsResponse{Type: "chunk", Content: chunk})
		}, func(thinking string) error {
			return websocket.JSON.Send(conn, wsResponse{Type: "thinking", Thinking: thinking})
		})
	}
	if err != nil {
		return fmt.Errorf("failed to generate response: %w", err)
	}
	return websocket.JSON.Send(conn, wsResponse{Type: "done", Answer: answer})
}

func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "closed") || strings.Contains(lower, "eof")
}

// healthHandler is a simple liveness probe (used in routes).
func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "pong"})
}
