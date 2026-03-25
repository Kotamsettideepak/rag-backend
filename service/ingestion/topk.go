package ingestion

import "strings"

func resolveTopK(question string, max int) (string, int) {
	norm := strings.ToLower(strings.TrimSpace(question))
	if norm == "" {
		return "simple", clampTopK(4, max)
	}
	score := 0
	if len(strings.Fields(norm)) >= 7 {
		score++
	}
	if len(strings.Fields(norm)) >= 14 {
		score++
	}
	for _, signal := range []string{"why", "how", "explain", "compare", "difference", "summarize", "summary", "relationship", "analyze", "analysis", "describe", "details", "step by step", "evidence", "overall"} {
		if strings.Contains(norm, signal) {
			score++
		}
	}
	switch {
	case score >= 3:
		return "complex", clampTopK(10, max)
	case score >= 1:
		return "medium", clampTopK(7, max)
	default:
		return "simple", clampTopK(4, max)
	}
}

func clampTopK(desired, max int) int {
	if desired <= 0 {
		desired = 1
	}
	if max <= 0 || desired > max {
		return desired
	}
	return desired
}
