package retrieval

import (
	"fmt"
	"strings"

	"gin-backend/model"
)

func filterMatches(matches []model.SearchMatch, keep func(model.SearchMatch) bool) []model.SearchMatch {
	out := make([]model.SearchMatch, 0, len(matches))
	for _, match := range matches {
		if keep(match) {
			out = append(out, match)
		}
	}
	return out
}

func metaStr(md map[string]interface{}, key string) string {
	if md == nil {
		return ""
	}
	v, ok := md[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func metaFloat(md map[string]interface{}, key string) (float64, bool) {
	if md == nil {
		return 0, false
	}
	switch v := md[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func metaInt(md map[string]interface{}, key string) (int, bool) {
	if md == nil {
		return 0, false
	}
	switch v := md[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}
