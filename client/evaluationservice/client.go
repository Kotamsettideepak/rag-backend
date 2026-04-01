package evaluationservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gin-backend/config"
)

type Job struct {
	QuizID          string `json:"quiz_id"`
	QuestionID      string `json:"question_id"`
	TopicID         string `json:"topic_id"`
	RequestedTopic  string `json:"requested_topic"`
	Question        string `json:"question"`
	UserAnswer      string `json:"user_answer"`
	CorrectAnswer   string `json:"correct_answer"`
	SupportingContext string `json:"supporting_context"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New() *Client {
	return &Client{
		baseURL: config.GetEvaluationServiceBaseURL(),
		token:   config.GetQuizInternalToken(),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) SubmitJob(ctx context.Context, job Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/evaluation/jobs", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.token) != "" {
		req.Header.Set("X-Internal-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("evaluation-service returned status %d", resp.StatusCode)
	}
	return nil
}
