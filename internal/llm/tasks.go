package llm

import (
	"context"
	"fmt"
)

// RunReview executes the code review task against the given code/diff.
func RunReview(ctx context.Context, client LLMClient, code string) (*ReviewResult, error) {
	raw, err := client.Complete(ctx, PromptMasterSystem, ReviewPrompt(code))
	if err != nil {
		return nil, fmt.Errorf("llm/task/review: %w", err)
	}
	var result ReviewResult
	if err := parseJSON(raw, &result); err != nil {
		return &ReviewResult{Summary: raw}, nil
	}
	return &result, nil
}

// RunBugAnalysis executes the bug analysis task.
// errMsg and expected can be empty strings if no specific error is known.
func RunBugAnalysis(ctx context.Context, client LLMClient, code, errMsg, expected string) (*BugResult, error) {
	raw, err := client.Complete(ctx, PromptMasterSystem, BugAnalysisPrompt(code, errMsg, expected))
	if err != nil {
		return nil, fmt.Errorf("llm/task/bug: %w", err)
	}
	var result BugResult
	if err := parseJSON(raw, &result); err != nil {
		return &BugResult{Explanation: raw}, nil
	}
	return &result, nil
}

// RunOptimization executes the code optimization task.
func RunOptimization(ctx context.Context, client LLMClient, code string) (*OptimizationResult, error) {
	raw, err := client.Complete(ctx, PromptMasterSystem, OptimizationPrompt(code))
	if err != nil {
		return nil, fmt.Errorf("llm/task/optimize: %w", err)
	}
	var result OptimizationResult
	if err := parseJSON(raw, &result); err != nil {
		return &OptimizationResult{Explanation: raw}, nil
	}
	return &result, nil
}

// RunLint executes the linting / standards enforcement task.
func RunLint(ctx context.Context, client LLMClient, code string) (*LintResult, error) {
	raw, err := client.Complete(ctx, PromptMasterSystem, LintPrompt(code))
	if err != nil {
		return nil, fmt.Errorf("llm/task/lint: %w", err)
	}
	var result LintResult
	if err := parseJSON(raw, &result); err != nil {
		return &LintResult{}, nil
	}
	return &result, nil
}
