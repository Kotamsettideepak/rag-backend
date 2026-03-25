package groq

func uniqueModels(primary string, fallbacks []string) []string {
	seen := make(map[string]struct{}, len(fallbacks)+1)
	out := make([]string, 0, len(fallbacks)+1)
	appendModel := func(model string) {
		if model == "" {
			return
		}
		if _, exists := seen[model]; exists {
			return
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	appendModel(primary)
	for _, model := range fallbacks {
		appendModel(model)
	}
	return out
}
