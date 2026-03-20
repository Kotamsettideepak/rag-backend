package api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gin-backend/llm"
	"gin-backend/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

type wsChatRequest struct {
	Type     string `json:"type"`
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

			if strings.TrimSpace(req.Question) == "" {
				if err := websocket.JSON.Send(conn, wsChatResponse{
					Type:    "error",
					Message: "question is required",
				}); err != nil {
					log.Printf("[ws] send validation error failed: %v", err)
					return
				}
				continue
			}

			if err := streamChatResponse(conn, strings.TrimSpace(req.Question)); err != nil {
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

func streamChatResponse(conn *websocket.Conn, question string) error {
	ctx, cancel := context.WithTimeout(conn.Request().Context(), 90*time.Second)
	defer cancel()

	contextText, err := service.DefaultManager().SearchContext(ctx, question)
	if err != nil {
		return fmt.Errorf("failed to search for context: %w", err)
	}

	if err := websocket.JSON.Send(conn, wsChatResponse{Type: "start"}); err != nil {
		return err
	}

	prompt := fmt.Sprintf(
		"You are answering questions only from the uploaded content.\n\nRetrieved context:\n%s\n\nUser question:\n%s\n\nInstructions:\n- Answer using only the retrieved context.\n- If the answer is not clearly present, say that it is not available in the uploaded content.\n- Be concise but complete.\n- When possible, quote exact values from the context.\n- If the question asks about file name, file type, extension, or document identity, prefer explicit uploaded file metadata over text printed inside the document body.\n- Do not confuse the uploaded filename with titles, internal chapter names, print marks, or layout/source file names appearing inside the document.",
		contextText,
		question,
	)

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
