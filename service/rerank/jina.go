package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"gin-backend/config"
	"gin-backend/model"
)

type Service struct {
	enabled    bool
	apiKeys    []string
	baseURL    string
	model      string
	httpClient *http.Client
	nextKey    atomic.Uint64
}

type request struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type response struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

func NewService(apiKeys []string) *Service {
	return &Service{
		enabled: config.IsJinaRerankEnabled(),
		apiKeys: append([]string(nil), apiKeys...),
		baseURL: config.GetJinaRerankBaseURL(),
		model:   config.GetJinaRerankModel(),
		httpClient: &http.Client{
			Timeout: config.GetJinaRerankTimeout(),
		},
	}
}

func (s *Service) Rank(ctx context.Context, query string, matches []model.SearchMatch) ([]model.SearchMatch, error) {
	query = strings.TrimSpace(query)
	if !s.enabled || query == "" || len(matches) <= 1 {
		return matches, nil
	}

	apiKey := s.pickAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return matches, fmt.Errorf("jina rerank api key is not configured")
	}

	documents := make([]string, 0, len(matches))
	for _, match := range matches {
		document := strings.TrimSpace(match.Document)
		if document == "" {
			document = "[empty]"
		}
		documents = append(documents, document)
	}

	payload, err := json.Marshal(request{
		Model:     s.model,
		Query:     query,
		Documents: documents,
		TopN:      len(documents),
	})
	if err != nil {
		return matches, fmt.Errorf("marshal jina rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, bytes.NewReader(payload))
	if err != nil {
		return matches, fmt.Errorf("create jina rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return matches, fmt.Errorf("send jina rerank request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return matches, fmt.Errorf("read jina rerank response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return matches, fmt.Errorf("jina rerank request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded response
	if err := json.Unmarshal(body, &decoded); err != nil {
		return matches, fmt.Errorf("decode jina rerank response: %w", err)
	}
	if len(decoded.Results) == 0 {
		return matches, fmt.Errorf("jina rerank returned no results")
	}

	ranked := make([]model.SearchMatch, 0, len(matches))
	used := make(map[int]struct{}, len(decoded.Results))
	for _, result := range decoded.Results {
		if result.Index < 0 || result.Index >= len(matches) {
			continue
		}
		used[result.Index] = struct{}{}
		match := matches[result.Index]
		match.Score = result.RelevanceScore
		ranked = append(ranked, match)
	}

	for index, match := range matches {
		if _, exists := used[index]; exists {
			continue
		}
		ranked = append(ranked, match)
	}

	if len(ranked) == 0 {
		return matches, fmt.Errorf("jina rerank produced an empty ranking")
	}
	return ranked, nil
}

func (s *Service) pickAPIKey() string {
	if len(s.apiKeys) == 0 {
		return ""
	}
	index := s.nextKey.Add(1) - 1
	return s.apiKeys[index%uint64(len(s.apiKeys))]
}
