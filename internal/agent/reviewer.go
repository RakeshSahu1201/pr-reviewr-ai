// Package agent contains the AI reviewer orchestration logic.
// It is deliberately isolated from any git-platform SDK or database driver.
package agent

import (
	"context"
	"fmt"
	"log"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	"pr-reviewer-ai/internal/repository"
	"strings"
)

// Reviewer orchestrates the code-review workflow.
// All platform interaction occurs through the git.GitProvider interface.
// If a Pipeline is provided, the full LLM analysis runs; otherwise a stub is used.
type Reviewer struct {
	provider  git.GitProvider
	logRepo   repository.ReviewLogRepository // optional audit log
	projectID string
	pipeline  *llm.Pipeline // optional; nil → stub analyser
}

// New creates a Reviewer with the supplied dependencies.
// logRepo and pipeline may both be nil.
func New(
	provider git.GitProvider,
	logRepo repository.ReviewLogRepository,
	projectID string,
	pipeline *llm.Pipeline,
) *Reviewer {
	return &Reviewer{
		provider:  provider,
		logRepo:   logRepo,
		projectID: projectID,
		pipeline:  pipeline,
	}
}

// Review runs the full review cycle for the given merge request:
//  1. Fetch the diff from the platform via GitProvider.FetchDiff
//  2. Analyse the diff (LLM pipeline or stub)
//  3. Post the generated review via GitProvider.PostReview
//  4. Log the review for audit (if logRepo is set)
func (r *Reviewer) Review(ctx context.Context, userID int64, mrID int) error {
	diff, err := r.provider.FetchDiff(mrID)
	if err != nil {
		return fmt.Errorf("agent: could not fetch diff for MR %d: %w", mrID, err)
	}

	comment, err := r.analyse(ctx, mrID, diff)
	if err != nil {
		return fmt.Errorf("agent: analysis failed for MR %d: %w", mrID, err)
	}

	log.Println("comment", comment)

	// if err := r.provider.PostReview(mrID, comment); err != nil {
	// 	return fmt.Errorf("agent: could not post review for MR %d: %w", mrID, err)
	// }

	// Optional audit log — does not fail the review if logging fails.
	// if r.logRepo != nil {
	// 	if logErr := r.logRepo.LogReview(userID, mrID, r.projectID, comment); logErr != nil {
	// 		fmt.Printf("agent: warning — failed to log review: %v\n", logErr)
	// 	}
	// }

	return nil
}

// analyse produces a review comment.
// When a Pipeline is configured it runs the full LLM flow;
// otherwise it falls back to a lightweight diff-stats stub.
func (r *Reviewer) analyse(ctx context.Context, mrID int, diff string) (string, error) {
	if r.pipeline != nil {
		result, err := r.pipeline.Run(ctx, diff)
		if err != nil {
			// LLM failure is non-fatal: fall through to stub.
			fmt.Printf("agent: LLM pipeline error (falling back to stub): %v\n", err)
		} else {
			return result.FormatAsMarkdown(mrID), nil
		}
	}

	// ── stub analyser (no LLM configured) ──
	lines := strings.Split(diff, "\n")
	added, removed := 0, 0
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			added++
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			removed++
		}
	}
	return fmt.Sprintf(
		"## 🤖 Automated Code Review — MR !%d\n\n"+
			"**Diff summary**: +%d lines added, -%d lines removed.\n\n"+
			"_No GEMINI_API_KEY configured. Set it to enable full AI analysis._",
		mrID, added, removed,
	), nil
}
