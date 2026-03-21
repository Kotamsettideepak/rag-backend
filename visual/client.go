package visual

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gin-backend/config"
	"gin-backend/models"
)

type Client interface {
	Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error)
}

type HTTPClient struct {
	baseURL string
	model   string
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

func NewHTTPClient() Client {
	return &HTTPClient{
		baseURL: config.GetGeminiBaseURL(),
		model:   config.GetGeminiModel(),
		apiKey:  config.GetGeminiAPIKey(),
		client:  &http.Client{Timeout: config.GetGeminiTimeout()},
	}
}

func (c *HTTPClient) Extract(ctx context.Context, staged models.StagedFile) (models.ParsedDocument, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return models.ParsedDocument{}, fmt.Errorf("GEMINI_API_KEY is not configured")
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
		return models.ParsedDocument{}, err
	}

	requestBody, err := json.Marshal(buildGenerateContentRequest(fileData, detectImageMIMEType(staged)))
	if err != nil {
		return models.ParsedDocument{}, err
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, c.model, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return models.ParsedDocument{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	log.Printf(
		"[visual] sending Gemini request url=%s/models/%s:generateContent filename=%s model=%s mime_type=%s bytes=%d",
		c.baseURL,
		c.model,
		filepath.Base(staged.OriginalName),
		c.model,
		detectImageMIMEType(staged),
		len(fileData),
	)

	resp, err := c.client.Do(req)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	log.Printf("[visual] response status=%d file=%s body=%s", resp.StatusCode, staged.OriginalName, strings.TrimSpace(string(responseBody)))
	if resp.StatusCode != http.StatusOK {
		return models.ParsedDocument{}, fmt.Errorf("gemini image analysis failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed generateContentResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return models.ParsedDocument{}, err
	}

	result, err := parseGeminiResponse(parsed)
	if err != nil {
		return models.ParsedDocument{}, err
	}
	log.Printf(
		"[visual] parsed Gemini response file=%s objects=%d colors=%d relationships=%d text_in_image=%d caption_present=%t description_present=%t summary_present=%t",
		staged.OriginalName,
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

	return models.ParsedDocument{
		FileID:    staged.FileID,
		FileName:  staged.OriginalName,
		FileKind:  staged.DetectedKind,
		Text:      documentText,
		PageTexts: pageTexts,
		ChatID:    staged.ChatID,
		UserID:    staged.UserID,
	}, nil
}

func buildImageTexts(staged models.StagedFile, response imageAnalysisResult) (string, []string) {
	metadata := buildImageMetadataBlock(staged)
	analysis := buildImageAnalysisBlock(response)

	pageTexts := []string{metadata}
	if analysis != "" {
		pageTexts = append(pageTexts, analysis)
	}

	text := strings.TrimSpace(strings.Join(pageTexts, "\n\n"))
	if text == "" {
		text = metadata
	}

	return text, pageTexts
}

func buildImageMetadataBlock(staged models.StagedFile) string {
	return strings.TrimSpace(strings.Join([]string{
		"Uploaded Image Metadata",
		"Actual uploaded filename: " + strings.TrimSpace(staged.OriginalName),
		"Image format: " + strings.ToUpper(strings.TrimPrefix(strings.ToLower(filepath.Ext(staged.OriginalName)), ".")),
		"Detected file type: " + strings.ToUpper(strings.TrimSpace(staged.DetectedKind)),
		"Content-Type: " + strings.TrimSpace(staged.ContentType),
	}, "\n"))
}

func buildImageAnalysisBlock(response imageAnalysisResult) string {
	parts := make([]string, 0, 7)

	if len(response.Objects) > 0 {
		parts = append(parts, "Objects present: "+strings.Join(cleanValues(response.Objects), ", "))
	}
	if len(response.Colors) > 0 {
		parts = append(parts, "Colors: "+strings.Join(cleanValues(response.Colors), ", "))
	}
	if caption := strings.TrimSpace(response.Caption); caption != "" {
		parts = append(parts, "Caption: "+caption)
	}
	if len(response.Relationships) > 0 {
		parts = append(parts, "Relationships: "+strings.Join(cleanValues(response.Relationships), "; "))
	}
	if len(response.TextInImage) > 0 {
		parts = append(parts, "Text in image: "+strings.Join(cleanValues(response.TextInImage), "; "))
	}
	if description := strings.TrimSpace(response.DetailedDescription); description != "" {
		parts = append(parts, "Detailed description: "+description)
	}
	if summary := strings.TrimSpace(response.ContextSummary); summary != "" {
		parts = append(parts, "Summary: "+summary)
	}

	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func cleanValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func buildGenerateContentRequest(fileData []byte, mimeType string) generateContentRequest {
	prompt := strings.TrimSpace(`You are an image analysis system.

Return valid JSON only with these fields:
- detailed_description: string
- objects: array of strings
- colors: array of strings
- caption: string
- relationships: array of strings
- text_in_image: array of strings
- context_summary: string

Be concrete, concise, and factual.
List dominant or important visible colors.
Write a short natural-language caption.
If no visible text exists, return an empty array for text_in_image.
Do not include markdown fences or any extra commentary.`)

	return generateContentRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
					{
						InlineData: &geminiInlineData{
							MIMEType: mimeType,
							Data:     encodeBase64(fileData),
						},
					},
				},
			},
		},
	}
}

func parseGeminiResponse(response generateContentResponse) (imageAnalysisResult, error) {
	if len(response.Candidates) == 0 {
		return imageAnalysisResult{}, fmt.Errorf("gemini returned no candidates")
	}

	rawOutput := extractCandidateText(response.Candidates[0])
	if strings.TrimSpace(rawOutput) == "" {
		return imageAnalysisResult{}, fmt.Errorf("gemini returned empty analysis text")
	}

	parsed, err := parseAnalysisJSON(rawOutput)
	if err != nil {
		return imageAnalysisResult{}, err
	}

	return imageAnalysisResult{
		DetailedDescription: normalizeString(parsed["detailed_description"]),
		Objects:             normalizeStringSlice(parsed["objects"]),
		Colors:              normalizeStringSlice(parsed["colors"]),
		Caption:             normalizeString(parsed["caption"]),
		Relationships:       normalizeStringSlice(parsed["relationships"]),
		TextInImage:         normalizeStringSlice(parsed["text_in_image"]),
		ContextSummary:      normalizeString(parsed["context_summary"]),
	}, nil
}

func extractCandidateText(candidate geminiCandidate) string {
	parts := make([]string, 0, len(candidate.Content.Parts))
	for _, part := range candidate.Content.Parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func parseAnalysisJSON(raw string) (map[string]any, error) {
	cleaned := cleanModelOutput(raw)
	candidates := []string{cleaned}

	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		candidates = append(candidates, cleaned[start:end+1])
	}

	for _, candidate := range candidates {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(candidate), &parsed); err == nil {
			return parsed, nil
		}
	}

	return nil, fmt.Errorf("gemini returned non-JSON analysis: %s", raw)
}

func cleanModelOutput(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}
	return strings.TrimSpace(cleaned)
}

func normalizeString(value any) string {
	if value == nil {
		return ""
	}
	text := strings.Join(strings.Fields(fmt.Sprintf("%v", value)), " ")
	return strings.TrimSpace(text)
}

func normalizeStringSlice(value any) []string {
	switch typed := value.(type) {
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text := normalizeString(item)
			if text != "" {
				items = append(items, text)
			}
		}
		return items
	case []string:
		return cleanValues(typed)
	case string:
		text := normalizeString(typed)
		if text == "" {
			return nil
		}
		return []string{text}
	default:
		return nil
	}
}

func detectImageMIMEType(staged models.StagedFile) string {
	if contentType := strings.TrimSpace(staged.ContentType); strings.HasPrefix(contentType, "image/") {
		return contentType
	}

	extension := strings.ToLower(filepath.Ext(staged.OriginalName))
	if extension != "" {
		if contentType := mime.TypeByExtension(extension); strings.HasPrefix(contentType, "image/") {
			return contentType
		}
	}

	if slices.Contains([]string{".jpg", ".jpeg"}, extension) {
		return "image/jpeg"
	}

	return "image/png"
}

func encodeBase64(fileData []byte) string {
	return base64.StdEncoding.EncodeToString(fileData)
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
