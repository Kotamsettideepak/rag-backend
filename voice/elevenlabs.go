package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"gin-backend/config"
)

type Client struct {
	baseURL      string
	apiKey       string
	voiceID      string
	ttsModel     string
	sttModel     string
	outputFormat string
	client       *http.Client
}

type SpeechToTextResponse struct {
	Text string `json:"text"`
}

type TextToSpeechResponse struct {
	AudioBase64 string
	MimeType    string
}

func NewClient() *Client {
	return &Client{
		baseURL:      config.GetElevenLabsBaseURL(),
		apiKey:       config.GetElevenLabsAPIKey(),
		voiceID:      config.GetElevenLabsVoiceID(),
		ttsModel:     config.GetElevenLabsTTSModel(),
		sttModel:     config.GetElevenLabsSTTModel(),
		outputFormat: config.GetElevenLabsOutputFormat(),
		client:       &http.Client{Timeout: config.GetElevenLabsTimeout()},
	}
}

func (c *Client) Transcribe(ctx context.Context, fileName string, payload []byte, contentType string) (SpeechToTextResponse, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return SpeechToTextResponse{}, fmt.Errorf("ELEVENLABS_API_KEY is required")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(fileName))
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	if _, err := part.Write(payload); err != nil {
		return SpeechToTextResponse{}, err
	}
	if err := writer.WriteField("model_id", c.sttModel); err != nil {
		return SpeechToTextResponse{}, err
	}
	if err := writer.Close(); err != nil {
		return SpeechToTextResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/speech-to-text", body)
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("xi-api-key", c.apiKey)
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return SpeechToTextResponse{}, fmt.Errorf("elevenlabs speech-to-text failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed SpeechToTextResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return SpeechToTextResponse{}, err
	}
	if strings.TrimSpace(parsed.Text) == "" {
		return SpeechToTextResponse{}, fmt.Errorf("elevenlabs returned empty transcript")
	}

	return parsed, nil
}

func (c *Client) Synthesize(ctx context.Context, text string) (TextToSpeechResponse, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return TextToSpeechResponse{}, fmt.Errorf("ELEVENLABS_API_KEY is required")
	}

	requestBody := map[string]interface{}{
		"text":     strings.TrimSpace(text),
		"model_id": c.ttsModel,
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return TextToSpeechResponse{}, err
	}

	endpoint := fmt.Sprintf(
		"%s/text-to-speech/%s/stream?output_format=%s",
		c.baseURL,
		c.voiceID,
		c.outputFormat,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	defer resp.Body.Close()

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return TextToSpeechResponse{}, fmt.Errorf("elevenlabs text-to-speech failed with status %d: %s", resp.StatusCode, string(audioBytes))
	}

	return TextToSpeechResponse{
		AudioBase64: base64.StdEncoding.EncodeToString(audioBytes),
		MimeType:    "audio/mpeg",
	}, nil
}
