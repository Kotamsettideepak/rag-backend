package chat

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"gin-backend/middleware"
	chatservice "gin-backend/service/chat"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

type wsRequest struct {
	Type     string `json:"type"`
	ChatID   string `json:"chat_id"`
	Question string `json:"question"`
}

type wsResponse struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
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
			question := strings.TrimSpace(req.Question)
			if chatID == "" || question == "" {
				_ = websocket.JSON.Send(conn, wsResponse{Type: "error", Message: "chat_id and question are required"})
				continue
			}

			if err := streamAnswer(conn, chatID, question); err != nil {
				log.Printf("[ws] stream error: %v", err)
				_ = websocket.JSON.Send(conn, wsResponse{Type: "error", Message: err.Error()})
			}
		}
	}).ServeHTTP(c.Writer, c.Request)
}

func streamAnswer(conn *websocket.Conn, chatID, question string) error {
	user, err := middleware.ResolveUserFromRequest(conn.Request())
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}

	if err := websocket.JSON.Send(conn, wsResponse{Type: "start"}); err != nil {
		return err
	}
	answer, err := chatservice.Default().StreamAnswer(context.Background(), user.ID, chatID, question, func(chunk string) error {
		return websocket.JSON.Send(conn, wsResponse{Type: "chunk", Content: chunk})
	})
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
