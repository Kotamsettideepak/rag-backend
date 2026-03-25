package deepgram

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gin-backend/config"
)

type Client struct {
	baseURL     string
	apiKey      string
	ttsModel    string
	sttModel    string
	ttsEncoding string
	client      *http.Client
}

type SpeechToTextResponse struct {
	Text string `json:"text"`
}

type TextToSpeechResponse struct {
	AudioBase64 string
	MimeType    string
}

type deepgramListenResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

func NewClient() *Client {
	return &Client{
		baseURL:     config.GetDeepgramBaseURL(),
		apiKey:      config.GetDeepgramAPIKey(),
		ttsModel:    config.GetDeepgramTTSModel(),
		sttModel:    config.GetDeepgramSTTModel(),
		ttsEncoding: config.GetDeepgramTTSEncoding(),
		client:      &http.Client{Timeout: config.GetDeepgramTimeout()},
	}
}

func (c *Client) Transcribe(ctx context.Context, fileName string, payload []byte, contentType string) (SpeechToTextResponse, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return SpeechToTextResponse{}, fmt.Errorf("DEEPGRAM_API_KEY is required")
	}
	if len(payload) == 0 {
		return SpeechToTextResponse{}, fmt.Errorf("deepgram transcription payload is empty")
	}

	endpoint := c.baseURL + "/listen?" + buildDeepgramSTTQuery(c.sttModel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return SpeechToTextResponse{}, err
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", normalizeAudioContentType(contentType))
	req.Header.Set("Accept", "application/json")

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
		return SpeechToTextResponse{}, fmt.Errorf("deepgram speech-to-text failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed deepgramListenResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return SpeechToTextResponse{}, err
	}

	transcript := extractDeepgramTranscript(parsed)
	if transcript == "" {
		return SpeechToTextResponse{}, fmt.Errorf("deepgram returned empty transcript")
	}

	return SpeechToTextResponse{Text: transcript}, nil
}

func (c *Client) Synthesize(ctx context.Context, text string) (TextToSpeechResponse, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return TextToSpeechResponse{}, fmt.Errorf("DEEPGRAM_API_KEY is required")
	}

	payload := map[string]string{
		"text": strings.TrimSpace(text),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return TextToSpeechResponse{}, err
	}

	endpoint := c.baseURL + "/speak?" + buildDeepgramTTSQuery(c.ttsModel, c.ttsEncoding)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return TextToSpeechResponse{}, err
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

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
		return TextToSpeechResponse{}, fmt.Errorf("deepgram text-to-speech failed with status %d: %s", resp.StatusCode, string(audioBytes))
	}

	return TextToSpeechResponse{
		AudioBase64: base64.StdEncoding.EncodeToString(audioBytes),
		MimeType:    deepgramMimeType(c.ttsEncoding),
	}, nil
}

func buildDeepgramSTTQuery(model string) string {
	values := url.Values{}
	values.Set("model", strings.TrimSpace(model))
	values.Set("smart_format", "true")
	values.Set("punctuate", "true")
	return values.Encode()
}

func buildDeepgramTTSQuery(model string, encoding string) string {
	values := url.Values{}
	values.Set("model", strings.TrimSpace(model))
	values.Set("encoding", strings.TrimSpace(encoding))
	return values.Encode()
}

func extractDeepgramTranscript(response deepgramListenResponse) string {
	if len(response.Results.Channels) == 0 {
		return ""
	}
	if len(response.Results.Channels[0].Alternatives) == 0 {
		return ""
	}

	return strings.TrimSpace(response.Results.Channels[0].Alternatives[0].Transcript)
}

func normalizeAudioContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return "audio/mpeg"
	}
	return contentType
}

func deepgramMimeType(encoding string) string {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "linear16":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}
