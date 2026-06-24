package llm

import (
	"encoding/json"
	"strings"
)

// parseJSON robustly extracts JSON from an LLM response.
// It handles optional Markdown fences (```json) and conversational text
// by slicing the string from the first { or [ to the last } or ].
func parseJSON(raw string, v any) error {
	clean := strings.TrimSpace(raw)

	firstBrace := strings.IndexAny(clean, "{[")
	lastBrace := strings.LastIndexAny(clean, "}]")

	if firstBrace != -1 && lastBrace != -1 && lastBrace >= firstBrace {
		clean = clean[firstBrace : lastBrace+1]
	}

	return json.Unmarshal([]byte(clean), v)
}
