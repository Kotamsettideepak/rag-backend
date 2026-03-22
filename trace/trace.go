package trace

import (
	"fmt"
	"log"
	"strings"
)

func Start(scope string, details string) {
	log.Printf(separator("START", scope, details))
}

func End(scope string, details string) {
	log.Printf(separator("END", scope, details))
}

func Mark(scope string, details string) {
	log.Printf(separator("STEP", scope, details))
}

func separator(label string, scope string, details string) string {
	core := fmt.Sprintf(" %s %s ", strings.ToUpper(strings.TrimSpace(label)), strings.TrimSpace(scope))
	if strings.TrimSpace(details) != "" {
		core += fmt.Sprintf("| %s ", strings.TrimSpace(details))
	}

	width := 160
	if len(core) >= width {
		return core
	}

	remaining := width - len(core)
	left := remaining / 2
	right := remaining - left
	return strings.Repeat("=", left) + core + strings.Repeat("=", right)
}
