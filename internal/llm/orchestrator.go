package llm

import (
	"context"
	"fmt"
)

// Orchestrate runs the meta-orchestrator prompt, which decides which analysis
// tasks should be run against the given diff.
// Falls back to all four tasks if the LLM response cannot be parsed.
func Orchestrate(ctx context.Context, client LLMClient, diff string) ([]string, string, error) {
	allTasks := []string{"review", "bug", "optimize", "lint"}

	userPrompt := fmt.Sprintf("Given the code diff that was provided, decide which analysis tasks to run. Return the JSON decision.\n\nCode Diff:\n%s", diff)

	raw, err := client.Complete(ctx, PromptMetaOrchestrator, userPrompt)
	if err != nil {
		// Graceful fallback: run everything.
		return allTasks, "fallback: LLM unavailable, running all tasks", nil
	}

	var decision TaskDecision
	if err := parseJSON(raw, &decision); err != nil || len(decision.Tasks) == 0 {
		return allTasks, fmt.Sprintf("fallback: parse error (%v), running all tasks", err), nil
	}

	// Validate tasks — only accept known values.
	valid := map[string]bool{"review": true, "bug": true, "optimize": true, "lint": true}
	var chosen []string
	for _, t := range decision.Tasks {
		if valid[t] {
			chosen = append(chosen, t)
		}
	}
	if len(chosen) == 0 {
		return allTasks, "fallback: no valid tasks returned", nil
	}

	return chosen, decision.Reasoning, nil
}
