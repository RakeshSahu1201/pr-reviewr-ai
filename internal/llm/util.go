package llm

import (
	"encoding/json"
	"strings"
)

// parseJSON strips optional Markdown code fences that LLMs sometimes wrap
// their JSON output in (e.g. ```json ... ```), then unmarshals into v.
func parseJSON(raw string, v any) error {
	clean := strings.TrimSpace(raw)

	// Strip ```json ... ``` or ``` ... ``` wrappers.
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		if idx := strings.LastIndex(clean, "```"); idx != -1 {
			clean = clean[:idx]
		}
		clean = strings.TrimSpace(clean)
	}

	return json.Unmarshal([]byte(clean), v)
}
