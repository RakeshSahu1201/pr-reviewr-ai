package llm

import (
	"context"
	"sync"
)

// Pipeline is the top-level orchestrator that runs the full analysis flow:
//
//  1. Context Retrieval (RAG)  — BuildContext
//  2. Meta Prompt              — Orchestrate (task selection)
//  3. Parallel Execution       — RunReview, RunBugAnalysis, RunOptimization, RunLint
//  4. Merge Results            — AnalysisResult
type Pipeline struct {
	client LLMClient
}

// NewPipeline creates a Pipeline backed by the given LLMClient.
func NewPipeline(client LLMClient) *Pipeline {
	return &Pipeline{client: client}
}

// Run executes the full pipeline against the provided code diff and returns
// a merged AnalysisResult ready to be formatted and posted as an MR comment.
func (p *Pipeline) Run(ctx context.Context, diff string) (*AnalysisResult, error) {
	result := &AnalysisResult{}

	// ─────────────────────────────────────────────────────────
	// Stage 1: Context Retrieval (RAG)
	// ─────────────────────────────────────────────────────────
	if ctx.Err() == nil {
		ctxResult, err := BuildContext(ctx, p.client, diff)
		if err == nil {
			result.Context = ctxResult
		}
	}

	// ─────────────────────────────────────────────────────────
	// Stage 2: Meta Prompt — decide which tasks to run
	// ─────────────────────────────────────────────────────────
	tasks, reasoning, _ := Orchestrate(ctx, p.client)
	result.TasksRun = tasks
	result.Reasoning = reasoning

	// ─────────────────────────────────────────────────────────
	// Stage 3: Parallel Execution
	// ─────────────────────────────────────────────────────────
	type parallelResult struct {
		review   *ReviewResult
		bug      *BugResult
		optimize *OptimizationResult
		lint     *LintResult
	}
	var (
		mu sync.Mutex
		pr parallelResult
		wg sync.WaitGroup
	)

	for _, task := range tasks {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			switch t {
			case "review":
				r, err := RunReview(ctx, p.client, diff)
				if err == nil {
					mu.Lock()
					pr.review = r
					mu.Unlock()
				}
			case "bug":
				r, err := RunBugAnalysis(ctx, p.client, diff, "", "no errors expected")
				if err == nil {
					mu.Lock()
					pr.bug = r
					mu.Unlock()
				}
			case "optimize":
				r, err := RunOptimization(ctx, p.client, diff)
				if err == nil {
					mu.Lock()
					pr.optimize = r
					mu.Unlock()
				}
			case "lint":
				r, err := RunLint(ctx, p.client, diff)
				if err == nil {
					mu.Lock()
					pr.lint = r
					mu.Unlock()
				}
			}
		}(task)
	}
	wg.Wait()

	// ─────────────────────────────────────────────────────────
	// Stage 4: Merge Results
	// ─────────────────────────────────────────────────────────
	result.Review = pr.review
	result.BugAnalysis = pr.bug
	result.Optimization = pr.optimize
	result.Lint = pr.lint

	return result, nil
}
