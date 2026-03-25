package vector

import (
	"bytes"
	"io"
	"net/http"

	"gin-backend/config"
)

func (s *Repository) doRequest(method, url string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewBuffer(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := config.GetChromaAPIKey(); apiKey != "" {
		req.Header.Set("x-chroma-token", apiKey)
	}
	return s.client.Do(req)
}
