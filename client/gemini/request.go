package gemini

import (
	"encoding/base64"
	"strings"
)

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
		Contents: []geminiContent{{
			Parts: []geminiPart{
				{Text: prompt},
				{InlineData: &geminiInlineData{MIMEType: mimeType, Data: encodeBase64(fileData)}},
			},
		}},
	}
}

func encodeBase64(fileData []byte) string {
	return base64.StdEncoding.EncodeToString(fileData)
}
