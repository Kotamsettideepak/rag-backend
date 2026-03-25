package vector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gin-backend/config"
)

func (s *Repository) getCollectionID() (string, error) {
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
	return s.createCollectionLocked()
}

func (s *Repository) findCollectionIDLocked() (string, error) {
	resp, err := s.doRequest(http.MethodGet, collectionBaseURL(), nil)
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
	var collections []map[string]interface{}
	if len(responseBody) == 0 {
		return "", nil
	}
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

func (s *Repository) createCollectionLocked() (string, error) {
	createPayload, err := json.Marshal(map[string]string{"name": collectionName})
	if err != nil {
		return "", err
	}
	resp, err := s.doRequest(http.MethodPost, collectionBaseURL(), createPayload)
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

func (s *Repository) deleteCollectionLocked(collectionID string) error {
	targets := []string{collectionID, collectionName}
	var lastErr error
	for _, target := range targets {
		resp, err := s.doRequest(http.MethodDelete, collectionItemURL(target), nil)
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
	if remainingID, err := s.findCollectionIDLocked(); err != nil {
		if lastErr != nil {
			return fmt.Errorf("%v; recheck failed: %w", lastErr, err)
		}
		return err
	} else if remainingID != "" {
		if lastErr != nil {
			return fmt.Errorf("%v; collection %s still exists", lastErr, remainingID)
		}
		return fmt.Errorf("collection %s still exists after delete attempts", remainingID)
	}
	return nil
}

func collectionBaseURL() string {
	return fmt.Sprintf(
		"%s/api/v2/tenants/%s/databases/%s/collections",
		config.GetChromaBaseURL(),
		config.GetChromaTenant(),
		config.GetChromaDatabase(),
	)
}

func collectionItemURL(target string) string {
	return collectionBaseURL() + "/" + target
}

func isCollectionNotFoundResponse(body string) bool {
	body = strings.ToLower(strings.TrimSpace(body))
	return strings.Contains(body, "notfounderror") ||
		strings.Contains(body, "does not exist") ||
		strings.Contains(body, "not found")
}
