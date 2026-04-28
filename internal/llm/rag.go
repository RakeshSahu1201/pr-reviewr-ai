package llm

import (
	"context"
	"fmt"
)

// BuildContext is the RAG stage: it summarises the diff to provide
// architectural context before the individual analysis tasks run.
func BuildContext(ctx context.Context, client LLMClient, diff string) (*ContextResult, error) {
	userPrompt := fmt.Sprintf("Analyse this code diff and return the JSON summary:\n\n%s", diff)

	raw, err := client.Complete(ctx, PromptContextBuilder, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("llm/rag: context retrieval failed: %w", err)
	}

	var result ContextResult
	if err := parseJSON(raw, &result); err != nil {
		// Return a best-effort result with the raw text as the summary.
		return &ContextResult{ArchitectureSummary: raw}, nil
	}
	return &result, nil
}
