package ingestion

import "strings"

func resolveTopK(question string, max int) (string, int, int) {
	norm := strings.ToLower(strings.TrimSpace(question))
	if norm == "" {
		return "simple", candidateTopK("simple", max), finalTopK("simple")
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
		return "complex", candidateTopK("complex", max), finalTopK("complex")
	case score >= 1:
		return "medium", candidateTopK("medium", max), finalTopK("medium")
	default:
		return "simple", candidateTopK("simple", max), finalTopK("simple")
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

func candidateTopK(level string, max int) int {
	switch level {
	case "complex":
		return clampTopK(envInt("QUERY_CANDIDATE_TOP_K_COMPLEX", 80), max)
	case "medium":
		return clampTopK(envInt("QUERY_CANDIDATE_TOP_K_MEDIUM", 60), max)
	default:
		return clampTopK(envInt("QUERY_CANDIDATE_TOP_K_SIMPLE", 40), max)
	}
}

func finalTopK(level string) int {
	switch level {
	case "complex":
		return envInt("QUERY_FINAL_TOP_K_COMPLEX", 16)
	case "medium":
		return envInt("QUERY_FINAL_TOP_K_MEDIUM", 12)
	default:
		return envInt("QUERY_FINAL_TOP_K_SIMPLE", 8)
	}
}
