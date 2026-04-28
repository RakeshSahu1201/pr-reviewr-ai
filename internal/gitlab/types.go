package gitlab

import "time"

// Project represents a GitLab project.
type Project struct {
	ID                int64
	Name              string
	PathWithNamespace string
	Description       string
	Visibility        string
	WebURL            string
	CreatedAt         *time.Time
}

// MergeRequest represents a GitLab Merge Request.
type MergeRequest struct {
	ID           int64
	IID          int64 // project-scoped MR number shown in the UI
	Title        string
	Description  string
	State        string // opened | closed | merged | locked
	SourceBranch string
	TargetBranch string
	AuthorName   string
	WebURL       string
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

// FileDiff represents the diff of a single changed file in an MR.
type FileDiff struct {
	OldPath       string
	NewPath       string
	Diff          string // unified diff string
	IsNewFile     bool
	IsRenamedFile bool
	IsDeletedFile bool
}

// Job represents a single CI/CD job in a GitLab pipeline.
type Job struct {
	ID         int64
	Name       string
	Status     string // created | pending | running | failed | success | canceled | skipped
	Stage      string
	StartedAt  *time.Time
	FinishedAt *time.Time
	Duration   float64 // seconds
	WebURL     string
	PipelineID int64
}
