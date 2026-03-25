package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gin-backend/config"
	"gin-backend/model"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Client interface {
	Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error)
}

type HTTPClient struct {
	baseURL string
	models  []string
	apiKey  string
	client  *http.Client
}

type generateContentRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
}

type generateContentResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type imageAnalysisResult struct {
	DetailedDescription string
	Objects             []string
	Colors              []string
	Caption             string
	Relationships       []string
	TextInImage         []string
	ContextSummary      string
}

func NewClient() Client {
	return &HTTPClient{
		baseURL: config.GetGeminiBaseURL(),
		models:  uniqueModels(config.GetGeminiModel(), config.GetGeminiFallbackModels()),
		apiKey:  config.GetGeminiAPIKey(),
		client:  &http.Client{Timeout: config.GetGeminiTimeout()},
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged model.StagedFile) (model.ParsedDocument, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return model.ParsedDocument{}, fmt.Errorf("GEMINI_API_KEY is not configured")
	}

	log.Printf(
		"[visual] preparing Gemini image analysis file=%s stored_path=%s content_type=%s kind=%s",
		staged.OriginalName,
		staged.StoredPath,
		staged.ContentType,
		staged.DetectedKind,
	)

	fileData, err := os.ReadFile(staged.StoredPath)
	if err != nil {
		return model.ParsedDocument{}, err
	}

	requestBody, err := json.Marshal(buildGenerateContentRequest(fileData, detectImageMIMEType(staged)))
	if err != nil {
		return model.ParsedDocument{}, err
	}

	result, usedModel, err := c.generateAnalysis(ctx, staged, requestBody)
	if err != nil {
		return model.ParsedDocument{}, err
	}
	log.Printf(
		"[visual] parsed Gemini response file=%s model=%s objects=%d colors=%d relationships=%d text_in_image=%d caption_present=%t description_present=%t summary_present=%t",
		staged.OriginalName,
		usedModel,
		len(result.Objects),
		len(result.Colors),
		len(result.Relationships),
		len(result.TextInImage),
		strings.TrimSpace(result.Caption) != "",
		strings.TrimSpace(result.DetailedDescription) != "",
		strings.TrimSpace(result.ContextSummary) != "",
	)

	documentText, pageTexts := buildImageTexts(staged, result)
	log.Printf(
		"[visual] extracted file=%s objects=%d colors=%d text_in_image=%d analysis_chars=%d preview=%s",
		staged.OriginalName,
		len(result.Objects),
		len(result.Colors),
		len(result.TextInImage),
		len(documentText),
		previewText(documentText, 220),
	)

	return model.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  staged.DetectedKind,
		Text:      documentText,
		PageTexts: pageTexts,
		ChatID:    staged.ChatID,
		UserID:    staged.UserID,
	}, nil
}

func (c *HTTPClient) generateAnalysis(ctx context.Context, staged model.StagedFile, requestBody []byte) (imageAnalysisResult, string, error) {
	var lastErr error
	for _, modelName := range c.models {
		endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, modelName, c.apiKey)
		log.Printf(
			"[visual] sending Gemini request url=%s/models/%s:generateContent filename=%s model=%s mime_type=%s bytes=%d",
			c.baseURL,
			modelName,
			filepath.Base(staged.OriginalName),
			modelName,
			detectImageMIMEType(staged),
			len(requestBody),
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
		if err != nil {
			return imageAnalysisResult{}, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", c.apiKey)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return imageAnalysisResult{}, "", ctx.Err()
			}
			log.Printf("[visual] Gemini model failed file=%s model=%s err=%v", staged.OriginalName, modelName, err)
			continue
		}

		responseBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return imageAnalysisResult{}, "", readErr
		}
		log.Printf("[visual] response status=%d file=%s model=%s body=%s", resp.StatusCode, staged.OriginalName, modelName, strings.TrimSpace(string(responseBody)))
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("gemini image analysis failed with status %d: %s", resp.StatusCode, string(responseBody))
			log.Printf("[visual] Gemini model failed file=%s model=%s status=%d", staged.OriginalName, modelName, resp.StatusCode)
			continue
		}

		var parsed generateContentResponse
		if err := json.Unmarshal(responseBody, &parsed); err != nil {
			lastErr = err
			continue
		}
		result, err := parseGeminiResponse(parsed)
		if err != nil {
			lastErr = err
			continue
		}
		return result, modelName, nil
	}
	return imageAnalysisResult{}, "", lastErr
}
