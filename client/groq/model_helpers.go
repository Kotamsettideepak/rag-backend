package groq

func firstModel(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[0]
}
