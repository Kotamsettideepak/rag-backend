package ingestion

import "strings"

func resolveTopK(question string, max int) (string, int, int) {
	return resolveTopKForScope(question, max, "query")
}

func resolveTopicTopK(question string, max int) (string, int, int) {
	return resolveTopKForScope(question, max, "topic")
}

func resolveTopKForScope(question string, max int, scope string) (string, int, int) {
	norm := strings.ToLower(strings.TrimSpace(question))
	if norm == "" {
		return "simple", candidateTopK(scope, "simple", max), finalTopK(scope, "simple")
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
		return "complex", candidateTopK(scope, "complex", max), finalTopK(scope, "complex")
	case score >= 1:
		return "medium", candidateTopK(scope, "medium", max), finalTopK(scope, "medium")
	default:
		return "simple", candidateTopK(scope, "simple", max), finalTopK(scope, "simple")
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

func candidateTopK(scope, level string, max int) int {
	if scope == "topic" {
		switch level {
		case "complex":
			return clampTopK(envInt("TOPIC_QUERY_CANDIDATE_TOP_K_COMPLEX", 140), max)
		case "medium":
			return clampTopK(envInt("TOPIC_QUERY_CANDIDATE_TOP_K_MEDIUM", 110), max)
		default:
			return clampTopK(envInt("TOPIC_QUERY_CANDIDATE_TOP_K_SIMPLE", 80), max)
		}
	}
	switch level {
	case "complex":
		return clampTopK(envInt("QUERY_CANDIDATE_TOP_K_COMPLEX", 80), max)
	case "medium":
		return clampTopK(envInt("QUERY_CANDIDATE_TOP_K_MEDIUM", 60), max)
	default:
		return clampTopK(envInt("QUERY_CANDIDATE_TOP_K_SIMPLE", 40), max)
	}
}

func finalTopK(scope, level string) int {
	if scope == "topic" {
		switch level {
		case "complex":
			return envInt("TOPIC_QUERY_FINAL_TOP_K_COMPLEX", 28)
		case "medium":
			return envInt("TOPIC_QUERY_FINAL_TOP_K_MEDIUM", 22)
		default:
			return envInt("TOPIC_QUERY_FINAL_TOP_K_SIMPLE", 16)
		}
	}
	switch level {
	case "complex":
		return envInt("QUERY_FINAL_TOP_K_COMPLEX", 16)
	case "medium":
		return envInt("QUERY_FINAL_TOP_K_MEDIUM", 12)
	default:
		return envInt("QUERY_FINAL_TOP_K_SIMPLE", 8)
	}
}
