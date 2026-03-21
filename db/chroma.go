package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"gin-backend/config"
	"gin-backend/models"
)

const collectionName = "rag_collection"

type ChromaStore struct {
	mu               sync.Mutex
	cachedCollection string
	client           *http.Client
}

type addRequest struct {
	IDs        []string                 `json:"ids"`
	Embeddings [][]float64              `json:"embeddings"`
	Metadatas  []map[string]interface{} `json:"metadatas"`
	Documents  []string                 `json:"documents"`
}

type queryRequest struct {
	QueryEmbeddings [][]float64 `json:"query_embeddings"`
	NResults        int         `json:"n_results"`
	Where           interface{} `json:"where,omitempty"`
}

type queryResponse struct {
	IDs       [][]string                 `json:"ids"`
	Documents [][]string                 `json:"documents"`
	Metadatas [][]map[string]interface{} `json:"metadatas"`
}

type getRequest struct {
	Where   interface{} `json:"where,omitempty"`
	Include []string    `json:"include,omitempty"`
	Limit   int         `json:"limit,omitempty"`
	Offset  int         `json:"offset,omitempty"`
}

type getResponse struct {
	IDs       []string                 `json:"ids"`
	Documents []string                 `json:"documents"`
	Metadatas []map[string]interface{} `json:"metadatas"`
}

func NewChromaStore() *ChromaStore {
	return &ChromaStore{
		client: &http.Client{
			Timeout: 90 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (s *ChromaStore) AddRecords(records []models.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}

	collectionID, err := s.getCollectionID()
	if err != nil {
		return err
	}

	payload := addRequest{
		IDs:        make([]string, 0, len(records)),
		Embeddings: make([][]float64, 0, len(records)),
		Metadatas:  make([]map[string]interface{}, 0, len(records)),
		Documents:  make([]string, 0, len(records)),
	}

	for _, record := range records {
		payload.IDs = append(payload.IDs, record.ID)
		payload.Embeddings = append(payload.Embeddings, record.Embedding)
		payload.Metadatas = append(payload.Metadatas, record.Metadata)
		payload.Documents = append(payload.Documents, record.Text)
	}
	log.Printf(
		"[chroma] add records collection=%s count=%d first_id=%s first_file=%v first_kind=%v first_content_type=%v",
		collectionName,
		len(records),
		records[0].ID,
		records[0].Metadata["file_name"],
		records[0].Metadata["file_kind"],
		records[0].Metadata["content_type"],
	)

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(
		"%s/api/v2/tenants/%s/databases/%s/collections/%s/add",
		config.GetChromaBaseURL(),
		config.GetChromaTenant(),
		config.GetChromaDatabase(),
		collectionID,
	)

	resp, err := s.doRequest(http.MethodPost, url, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chroma add failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	return nil
}

func (s *ChromaStore) Search(embedding []float64, nResults int, where map[string]interface{}) ([]models.SearchMatch, error) {
	collectionID, err := s.getCollectionID()
	if err != nil {
		return nil, err
	}

	normalizedWhere := normalizeWhereClause(where)
	req := queryRequest{
		QueryEmbeddings: [][]float64{embedding},
		NResults:        nResults,
		Where:           normalizedWhere,
	}
	log.Printf("[chroma] query collection=%s n_results=%d embedding_dims=%d where=%v normalized_where=%v", collectionName, nResults, len(embedding), where, normalizedWhere)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf(
		"%s/api/v2/tenants/%s/databases/%s/collections/%s/query",
		config.GetChromaBaseURL(),
		config.GetChromaTenant(),
		config.GetChromaDatabase(),
		collectionID,
	)

	resp, err := s.doRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("chroma query failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed queryResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode chroma query response: %w", err)
	}
	matches := buildSearchMatches(parsed)
	log.Printf(
		"[chroma] query results count=%d first_file=%s first_kind=%s first_content_type=%s first_preview=%s",
		len(matches),
		firstMatchMetadata(matches, "file_name"),
		firstMatchMetadata(matches, "file_kind"),
		firstMatchMetadata(matches, "content_type"),
		firstMatchPreview(matches),
	)
	return matches, nil
}

func (s *ChromaStore) GetByMetadata(where map[string]interface{}, limit int) ([]models.SearchMatch, error) {
	collectionID, err := s.getCollectionID()
	if err != nil {
		return nil, err
	}

	normalizedWhere := normalizeWhereClause(where)
	req := getRequest{
		Where:   normalizedWhere,
		Include: []string{"documents", "metadatas"},
		Limit:   limit,
		Offset:  0,
	}
	log.Printf("[chroma] get by metadata collection=%s where=%v normalized_where=%v limit=%d", collectionName, where, normalizedWhere, limit)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf(
		"%s/api/v2/tenants/%s/databases/%s/collections/%s/get",
		config.GetChromaBaseURL(),
		config.GetChromaTenant(),
		config.GetChromaDatabase(),
		collectionID,
	)

	resp, err := s.doRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("chroma get failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed getResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode chroma get response: %w", err)
	}
	matches := buildGetMatches(parsed)
	log.Printf("[chroma] get results count=%d first_file=%s", len(matches), firstMatchMetadata(matches, "file_name"))
	return matches, nil
}

func buildSearchMatches(response queryResponse) []models.SearchMatch {
	if len(response.Documents) == 0 {
		return nil
	}

	documents := response.Documents[0]
	var ids []string
	if len(response.IDs) > 0 {
		ids = response.IDs[0]
	}

	var metadatas []map[string]interface{}
	if len(response.Metadatas) > 0 {
		metadatas = response.Metadatas[0]
	}

	matches := make([]models.SearchMatch, 0, len(documents))
	for index, document := range documents {
		match := models.SearchMatch{Document: document}
		if index < len(ids) {
			match.ID = ids[index]
		}
		if index < len(metadatas) {
			match.Metadata = metadatas[index]
		}
		matches = append(matches, match)
	}

	return matches
}

func buildGetMatches(response getResponse) []models.SearchMatch {
	if len(response.Documents) == 0 {
		return nil
	}

	matches := make([]models.SearchMatch, 0, len(response.Documents))
	for index, document := range response.Documents {
		match := models.SearchMatch{Document: document}
		if index < len(response.IDs) {
			match.ID = response.IDs[index]
		}
		if index < len(response.Metadatas) {
			match.Metadata = response.Metadatas[index]
		}
		matches = append(matches, match)
	}

	return matches
}

func (s *ChromaStore) ClearCollection() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	collectionID, err := s.findCollectionIDLocked()
	if err != nil {
		return err
	}
	if collectionID == "" {
		s.cachedCollection = ""
		return nil
	}

	if err := s.deleteCollectionLocked(collectionID); err != nil {
		return err
	}

	s.cachedCollection = ""
	return nil
}

func (s *ChromaStore) getCollectionID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cachedCollection != "" {
		return s.cachedCollection, nil
	}

	foundID, err := s.findCollectionIDLocked()
	if err != nil {
		return "", err
	}
	if foundID != "" {
		s.cachedCollection = foundID
		return foundID, nil
	}

	createPayload, err := json.Marshal(map[string]string{"name": collectionName})
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf(
		"%s/api/v2/tenants/%s/databases/%s/collections",
		config.GetChromaBaseURL(),
		config.GetChromaTenant(),
		config.GetChromaDatabase(),
	)

	resp, err := s.doRequest(http.MethodPost, url, createPayload)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("chroma collection create failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", err
	}

	id, _ := parsed["id"].(string)
	if id == "" {
		return "", fmt.Errorf("collection id missing in response: %s", string(responseBody))
	}

	s.cachedCollection = id
	return id, nil
}

func (s *ChromaStore) findCollectionIDLocked() (string, error) {
	url := fmt.Sprintf(
		"%s/api/v2/tenants/%s/databases/%s/collections",
		config.GetChromaBaseURL(),
		config.GetChromaTenant(),
		config.GetChromaDatabase(),
	)

	resp, err := s.doRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chroma collection list failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	if len(responseBody) == 0 {
		return "", nil
	}

	var collections []map[string]interface{}
	if err := json.Unmarshal(responseBody, &collections); err != nil {
		return "", err
	}

	for _, collection := range collections {
		name, _ := collection["name"].(string)
		id, _ := collection["id"].(string)
		if name == collectionName && id != "" {
			return id, nil
		}
	}

	return "", nil
}

func (s *ChromaStore) doRequest(method string, url string, body []byte) (*http.Response, error) {
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

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *ChromaStore) deleteCollectionLocked(collectionID string) error {
	targets := []string{collectionID, collectionName}
	var lastErr error

	for _, target := range targets {
		url := fmt.Sprintf(
			"%s/api/v2/tenants/%s/databases/%s/collections/%s",
			config.GetChromaBaseURL(),
			config.GetChromaTenant(),
			config.GetChromaDatabase(),
			target,
		)

		resp, err := s.doRequest(http.MethodDelete, url, nil)
		if err != nil {
			lastErr = err
			continue
		}

		responseBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			return nil
		}

		if resp.StatusCode == http.StatusNotFound || isCollectionNotFoundResponse(string(responseBody)) {
			lastErr = fmt.Errorf("collection target %s not found", target)
			continue
		}

		lastErr = fmt.Errorf("chroma clear failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	remainingID, err := s.findCollectionIDLocked()
	if err != nil {
		if lastErr != nil {
			return fmt.Errorf("%v; recheck failed: %w", lastErr, err)
		}
		return err
	}

	if remainingID == "" {
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("%v; collection %s still exists", lastErr, remainingID)
	}

	return fmt.Errorf("collection %s still exists after delete attempts", remainingID)
}

func isCollectionNotFoundResponse(body string) bool {
	body = strings.ToLower(strings.TrimSpace(body))
	return strings.Contains(body, "notfounderror") ||
		strings.Contains(body, "does not exist") ||
		strings.Contains(body, "not found")
}

func firstMatchMetadata(matches []models.SearchMatch, key string) string {
	if len(matches) == 0 || matches[0].Metadata == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", matches[0].Metadata[key]))
}

func firstMatchPreview(matches []models.SearchMatch) string {
	if len(matches) == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(strings.TrimSpace(matches[0].Document)), " ")
	if len(text) <= 180 {
		return text
	}
	return text[:180] + "..."
}

func normalizeWhereClause(where map[string]interface{}) interface{} {
	if len(where) == 0 {
		return nil
	}

	if len(where) == 1 {
		for key, value := range where {
			return map[string]interface{}{
				key: map[string]interface{}{
					"$eq": value,
				},
			}
		}
	}

	clauses := make([]map[string]interface{}, 0, len(where))
	for key, value := range where {
		clauses = append(clauses, map[string]interface{}{
			key: map[string]interface{}{
				"$eq": value,
			},
		})
	}

	return map[string]interface{}{
		"$and": clauses,
	}
}
