package vector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"gin-backend/config"
)

func (s *Repository) ClearCollection() error {
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

func (s *Repository) DeleteByMetadata(where map[string]interface{}) error {
	collectionID, err := s.getCollectionID()
	if err != nil {
		return err
	}
	normalizedWhere := normalizeWhereClause(where)
	body, err := json.Marshal(deleteRequest{Where: normalizedWhere})
	if err != nil {
		return err
	}
	resp, err := s.doRequest(http.MethodPost, deleteURL(collectionID), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("chroma delete failed with status %d: %s", resp.StatusCode, string(responseBody))
	}
	return nil
}

func deleteURL(collectionID string) string {
	return fmt.Sprintf(
		"%s/api/v2/tenants/%s/databases/%s/collections/%s/delete",
		config.GetChromaBaseURL(),
		config.GetChromaTenant(),
		config.GetChromaDatabase(),
		collectionID,
	)
}
