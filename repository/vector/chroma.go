package vector

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"gin-backend/config"
	"gin-backend/model"
)

const collectionName = "rag_collection"

func (s *Repository) AddRecords(records []model.VectorRecord) error {
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

func (s *Repository) Search(embedding []float64, nResults int, where map[string]interface{}) ([]model.SearchMatch, error) {
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

func (s *Repository) GetByMetadata(where map[string]interface{}, limit int) ([]model.SearchMatch, error) {
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
