// Package git defines the GitProvider abstraction for interacting with
// any code-hosting platform (GitLab, GitHub, Gitea, or MCP local filesystem).
// No concrete platform SDK should ever be imported outside of this package.
package git

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

	// FetchRecentEvents fetches recent user contribution events that occurred after sinceEventID.
	// This acts as a polling alternative to webhooks.
	FetchRecentEvents(sinceEventID int) ([]UserEvent, error)

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
	ID          int64
	ActionName  string // e.g. "commented on", "opened", "pushed to"
	TargetType  string // e.g. "MergeRequest", "Issue"
	TargetTitle string // title of the MR or Issue
	Body        string // note/comment body
	Ref         string // branch name pushed to
}
