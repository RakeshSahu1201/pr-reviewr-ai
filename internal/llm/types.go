package llm

import (
	"fmt"
	"strings"
)

// ContextResult is returned by the RAG / context-builder stage.
type ContextResult struct {
	ArchitectureSummary string   `json:"architecture_summary"`
	KeyComponents       []string `json:"key_components"`
	DataFlow            string   `json:"data_flow"`
	RiskAreas           []string `json:"risk_areas"`
}

// Issue is a single finding from the code review task.
type Issue struct {
	Type        string `json:"type"` // bug | smell | readability | performance
	Description string `json:"description"`
	Line        string `json:"line"`
	Severity    string `json:"severity"` // low | medium | high
	Fix         string `json:"fix"`
}

// ReviewResult is returned by the code review task.
type ReviewResult struct {
	Issues  []Issue `json:"issues"`
	Summary string  `json:"summary"`
}

// BugResult is returned by the bug analysis task.
type BugResult struct {
	RootCause   string   `json:"root_cause"`
	Explanation string   `json:"explanation"`
	Fix         string   `json:"fix"`
	EdgeCases   []string `json:"edge_cases"`
}

// OptimizationResult is returned by the optimization task.
type OptimizationResult struct {
	CurrentComplexity string   `json:"current_complexity"`
	Issues            []string `json:"issues"`
	OptimizedCode     string   `json:"optimized_code"`
	Explanation       string   `json:"explanation"`
}

// Violation is a single finding from the linter task.
type Violation struct {
	Rule        string `json:"rule"`
	Description string `json:"description"`
	Fix         string `json:"fix"`
}

// LintResult is returned by the linting task.
type LintResult struct {
	Violations []Violation `json:"violations"`
}

// TaskDecision is the meta-orchestrator's output.
type TaskDecision struct {
	Tasks     []string `json:"tasks"`
	Reasoning string   `json:"reasoning"`
}

// AnalysisResult is the final merged output of the full pipeline.
type AnalysisResult struct {
	Context      *ContextResult      `json:"context,omitempty"`
	Review       *ReviewResult       `json:"review,omitempty"`
	BugAnalysis  *BugResult          `json:"bug_analysis,omitempty"`
	Optimization *OptimizationResult `json:"optimization,omitempty"`
	Lint         *LintResult         `json:"lint,omitempty"`
	TasksRun     []string            `json:"tasks_run"`
	Reasoning    string              `json:"reasoning,omitempty"`
}

// FormatAsMarkdown converts the pipeline result into a GitLab/GitHub-ready
// Markdown comment, suitable for posting directly on an MR.
func (r *AnalysisResult) FormatAsMarkdown(mrID int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## 🤖 AI Code Review — MR !%d\n\n", mrID))
	sb.WriteString(fmt.Sprintf("**Tasks run**: %s\n\n", strings.Join(r.TasksRun, ", ")))
	if r.Reasoning != "" {
		sb.WriteString(fmt.Sprintf("> **Orchestrator reasoning**: %s\n\n", r.Reasoning))
	}

	// --- Architecture Context ---
	if r.Context != nil {
		sb.WriteString("---\n### 🗺️ Architecture Context\n\n")
		sb.WriteString(fmt.Sprintf("**Summary**: %s\n\n", r.Context.ArchitectureSummary))
		if len(r.Context.RiskAreas) > 0 {
			sb.WriteString("**Risk areas**:\n")
			for _, ra := range r.Context.RiskAreas {
				sb.WriteString(fmt.Sprintf("- %s\n", ra))
			}
			sb.WriteString("\n")
		}
	}

	// --- Code Review ---
	if r.Review != nil {
		sb.WriteString("---\n### 🔍 Code Review\n\n")
		if len(r.Review.Issues) > 0 {
			for _, issue := range r.Review.Issues {
				emoji := severityEmoji(issue.Severity)
				sb.WriteString(fmt.Sprintf("#### %s `%s` — %s\n", emoji, issue.Type, issue.Description))
				if issue.Line != "" {
					sb.WriteString(fmt.Sprintf("**Line**: `%s`\n", issue.Line))
				}
				if issue.Fix != "" {
					sb.WriteString(fmt.Sprintf("**Fix**: %s\n\n", issue.Fix))
				}
			}
		}
		if r.Review.Summary != "" {
			sb.WriteString(fmt.Sprintf("**Summary**: %s\n\n", r.Review.Summary))
		}
	}

	// --- Bug Analysis ---
	if r.BugAnalysis != nil {
		sb.WriteString("---\n### 🐛 Bug Analysis\n\n")
		sb.WriteString(fmt.Sprintf("**Root cause**: %s\n\n", r.BugAnalysis.RootCause))
		sb.WriteString(fmt.Sprintf("**Explanation**: %s\n\n", r.BugAnalysis.Explanation))
		if r.BugAnalysis.Fix != "" {
			sb.WriteString(fmt.Sprintf("**Fix**: %s\n\n", r.BugAnalysis.Fix))
		}
		if len(r.BugAnalysis.EdgeCases) > 0 {
			sb.WriteString("**Edge cases to watch**:\n")
			for _, ec := range r.BugAnalysis.EdgeCases {
				sb.WriteString(fmt.Sprintf("- %s\n", ec))
			}
			sb.WriteString("\n")
		}
	}

	// --- Optimization ---
	if r.Optimization != nil {
		sb.WriteString("---\n### ⚡ Optimization\n\n")
		sb.WriteString(fmt.Sprintf("**Current complexity**: %s\n\n", r.Optimization.CurrentComplexity))
		sb.WriteString(fmt.Sprintf("**Explanation**: %s\n\n", r.Optimization.Explanation))
	}

	// --- Lint ---
	if r.Lint != nil && len(r.Lint.Violations) > 0 {
		sb.WriteString("---\n### 📏 Lint Violations\n\n")
		for _, v := range r.Lint.Violations {
			sb.WriteString(fmt.Sprintf("- **%s**: %s — _%s_\n", v.Rule, v.Description, v.Fix))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n_Generated by pr-reviewer-ai · Replace `analyse()` stub with your LLM key to activate._")
	return sb.String()
}

func severityEmoji(s string) string {
	switch s {
	case "high":
		return "🔴"
	case "medium":
		return "🟡"
	default:
		return "🟢"
	}
}
