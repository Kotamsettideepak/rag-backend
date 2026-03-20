package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
}

type queryResponse struct {
	IDs       [][]string                 `json:"ids"`
	Documents [][]string                 `json:"documents"`
	Metadatas [][]map[string]interface{} `json:"metadatas"`
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

func (s *ChromaStore) Search(embedding []float64, nResults int) ([]models.SearchMatch, error) {
	collectionID, err := s.getCollectionID()
	if err != nil {
		return nil, err
	}

	req := queryRequest{
		QueryEmbeddings: [][]float64{embedding},
		NResults:        nResults,
	}

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

	return buildSearchMatches(parsed), nil
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
