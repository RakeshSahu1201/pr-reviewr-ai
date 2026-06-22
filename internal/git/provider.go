// Package git defines the GitProvider abstraction for interacting with
// any code-hosting platform (GitLab, GitHub, Gitea, or MCP local filesystem).
// No concrete platform SDK should ever be imported outside of this package.
package git

import "time"

// GitProvider abstracts all git-platform operations needed by the reviewer agent.
// Concrete implementations (GitLab, GitHub, MCP adapter) satisfy this interface,
// and the agent layer depends only on this contract.
type GitProvider interface {
	// FetchDiff returns the unified diff for the merge/pull request identified by mrID.
	// The returned string is a standard unified diff that any diff parser can process.
	FetchDiff(mrID int) (string, error)

	// PostReview posts an automated review comment to the merge/pull request.
	// comment is the full text of the review that should appear on the platform.
	PostReview(mrID int, comment string) error

	// FetchRecentEvents fetches user contribution events that occurred after the given time.
	// Callers typically pass time.Now().Add(-5*time.Minute) so each poll only
	// processes the activity window since the last tick.
	FetchRecentEvents(since time.Time) ([]UserEvent, error)

	// ListProjects returns a list of projects accessible by the authenticated user.
	ListProjects() ([]Project, error)
}

// Project represents a generic git project/repository.
type Project struct {
	ID   int64
	Name string
}

// UserEvent represents an activity event across any generic platform.
type UserEvent struct {
	ActionName  string // e.g. "commented on", "opened", "pushed to"
	TargetType  string // e.g. "MergeRequest", "Issue"
	TargetTitle string // title of the MR or Issue
	Body        string // note/comment body
	Ref         string // branch name pushed to
	NoteableID  int64  // id of the noteable (e.g. MR or Issue)
	NoteableIID int64  // iid of the noteable (e.g. MR or Issue)
}
