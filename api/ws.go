package api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gin-backend/ingest"
	"gin-backend/llm"
	"gin-backend/store"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

type wsChatRequest struct {
	Type     string `json:"type"`
	ChatID   string `json:"chat_id"`
	Question string `json:"question"`
}

type wsChatResponse struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Answer  string `json:"answer,omitempty"`
	Message string `json:"message,omitempty"`
}

func ChatWebSocketHandler(c *gin.Context) {
	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		conn.PayloadType = websocket.TextFrame

		for {
			var req wsChatRequest
			if err := websocket.JSON.Receive(conn, &req); err != nil {
				if !isWebsocketClosed(err) {
					log.Printf("[ws] receive failed: %v", err)
				}
				return
			}

			if strings.TrimSpace(req.ChatID) == "" || strings.TrimSpace(req.Question) == "" {
				if err := websocket.JSON.Send(conn, wsChatResponse{
					Type:    "error",
					Message: "chat_id and question are required",
				}); err != nil {
					log.Printf("[ws] send validation error failed: %v", err)
					return
				}
				continue
			}

			if err := streamChatResponse(conn, strings.TrimSpace(req.ChatID), strings.TrimSpace(req.Question)); err != nil {
				log.Printf("[ws] stream failed: %v", err)
				if sendErr := websocket.JSON.Send(conn, wsChatResponse{
					Type:    "error",
					Message: err.Error(),
				}); sendErr != nil {
					log.Printf("[ws] send error message failed: %v", sendErr)
					return
				}
			}
		}
	}).ServeHTTP(c.Writer, c.Request)
}

func streamChatResponse(conn *websocket.Conn, chatID string, question string) error {
	ctx, cancel := context.WithTimeout(conn.Request().Context(), 90*time.Second)
	defer cancel()

	user, err := resolveCurrentUserFromRequest(conn.Request())
	if err != nil {
		return fmt.Errorf("failed to resolve google user: %w", err)
	}
	if _, err := store.DefaultStore().GetChat(ctx, chatID, user.ID); err != nil {
		return fmt.Errorf("chat not found")
	}
	if _, err := store.DefaultStore().SaveMessage(ctx, chatID, "user", question); err != nil {
		return fmt.Errorf("failed to save user message: %w", err)
	}

	contextResult, err := ingest.DefaultManager().SearchContext(ctx, question, chatID, user.ID)
	if err != nil {
		return fmt.Errorf("failed to search for context: %w", err)
	}

	if err := websocket.JSON.Send(conn, wsChatResponse{Type: "start"}); err != nil {
		return err
	}

	log.Printf("[ws] context modality=%s", contextResult.Modality)
	previousMessages, err := store.DefaultStore().ListMessages(ctx, chatID, 10)
	if err != nil {
		return fmt.Errorf("failed to load messages: %w", err)
	}
	prompt := buildPrompt(contextResult.Modality, contextResult.Context, buildConversationContext(previousMessages), question)

	client := llm.NewGroqClient()
	stream := make(chan string)
	done := make(chan error, 1)
	go func() {
		done <- client.StreamResponse([]llm.Message{
			{Role: "user", Content: prompt},
		}, stream)
	}()

	var answerBuilder strings.Builder
	for {
		select {
		case chunk := <-stream:
			answerBuilder.WriteString(chunk)
			if err := websocket.JSON.Send(conn, wsChatResponse{
				Type:    "chunk",
				Content: chunk,
			}); err != nil {
				return err
			}
		case err := <-done:
			if err != nil {
				return fmt.Errorf("failed to generate text response: %w", err)
			}

			if _, saveErr := store.DefaultStore().SaveMessage(ctx, chatID, "assistant", answerBuilder.String()); saveErr != nil {
				return fmt.Errorf("failed to save assistant message: %w", saveErr)
			}

			return websocket.JSON.Send(conn, wsChatResponse{
				Type:   "done",
				Answer: answerBuilder.String(),
			})
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func isWebsocketClosed(err error) bool {
	if err == nil {
		return false
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "closed") || strings.Contains(lower, "eof")
}
