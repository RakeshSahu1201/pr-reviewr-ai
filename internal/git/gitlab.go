package git

import (
	"fmt"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// GitLabProvider implements GitProvider using the GitLab REST API.
// It is the only file in the entire codebase that imports a GitLab-specific SDK.
type GitLabProvider struct {
	client       *gitlab.Client
	projectID    int64
	gitlabUserID int
}

// NewGitLabProvider constructs a GitLabProvider.
//
//	baseURL   – GitLab instance URL, e.g. "https://gitlab.com"
//	token     – Personal Access Token with api scope
//	projectID – GitLab project numeric ID
func NewGitLabProvider(baseURL, token string, projectID int64, gitlabUserID int) (*GitLabProvider, error) {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("git: failed to create GitLab client: %w", err)
	}
	return &GitLabProvider{
		client:       client,
		projectID:    projectID,
		gitlabUserID: gitlabUserID,
	}, nil
}

// FetchDiff retrieves the unified diff for the given merge request.
// Uses ListMergeRequestDiffs (replaces deprecated GetMergeRequestChanges).
func (g *GitLabProvider) FetchDiff(mrID int) (string, error) {
	var sb strings.Builder
	for {
		diffs, resp, err := g.client.MergeRequests.ListMergeRequestDiffs(g.projectID, int64(mrID), nil)
		if err != nil {
			return "", fmt.Errorf("git: FetchDiff MR %d: %w", mrID, err)
		}
		for _, d := range diffs {
			sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", d.OldPath, d.NewPath))
			sb.WriteString(d.Diff)
			sb.WriteString("\n")
		}
		if resp.NextPage == 0 {
			break
		}
	}
	return sb.String(), nil
}

// PostReview creates a general note on the merge request with the provided comment.
func (g *GitLabProvider) PostReview(mrID int, comment string) error {
	opts := &gitlab.CreateMergeRequestNoteOptions{
		Body: gitlab.Ptr(comment),
	}
	_, _, err := g.client.Notes.CreateMergeRequestNote(g.projectID, int64(mrID), opts)
	if err != nil {
		return fmt.Errorf("git: PostReview MR %d: %w", mrID, err)
	}
	return nil
}

// FetchRecentEvents fetches contribution events performed by the authenticated user
// that occurred after `since`. Callers pass time.Now().Add(-5*time.Minute) so each
// poll only surfaces activity from the last polling window.
func (g *GitLabProvider) FetchRecentEvents(since time.Time) ([]UserEvent, error) {
	// The GitLab API 'after' parameter for events only supports date-level precision (YYYY-MM-DD).
	// To get today's events, we must request events starting from yesterday and then
	// filter them by the exact timestamp in-memory.

	// 2. Parse it using the GitLab helper
	// This helper expects "2006-01-02"
	after, _ := gitlab.ParseISOTime(time.Now().AddDate(0, 0, -1).Format("2006-01-02"))
	// after, _ := gitlab.ParseISOTime(since.Format("2006-01-02"))
	opts := &gitlab.ListContributionEventsOptions{
		After: &after,
		ListOptions: gitlab.ListOptions{
			PerPage: 50,
			Page:    1,
		},
	}

	var events []*gitlab.ContributionEvent
	var err error

	if g.gitlabUserID > 0 {
		// Use the user-specific events API as requested: api/v4/users/:id/events
		events, _, err = g.client.Users.ListUserContributionEvents(g.gitlabUserID, opts)
	} else {
		// Fallback to the authenticated user's events if ID is missing.
		events, _, err = g.client.Events.ListCurrentUserContributionEvents(opts)
	}

	if err != nil {
		return nil, fmt.Errorf("git: FetchRecentEvents: %w", err)
	}

	var results []UserEvent
	for _, event := range events {
		// In-memory filter to ensure we only return events within the precise window.
		if event.CreatedAt == nil || event.CreatedAt.Before(since) {
			continue
		}

		userEvent := UserEvent{
			ActionName:  event.ActionName,
			TargetType:  event.TargetType,
			TargetTitle: event.TargetTitle,
			NoteableID:  event.Note.NoteableID,
			NoteableIID: event.Note.NoteableIID,
		}
		if event.TargetType == "Note" && event.Note != nil {
			userEvent.Body = event.Note.Body
			// For comments, GitLab sets TargetType to "Note" or "DiffNote",
			// but we want the logical target type (e.g. "MergeRequest")
			// if event.Note.NoteableType != "" {
			// 	userEvent.TargetType = event.Note.NoteableType
			// }
		}
		userEvent.Ref = event.PushData.Ref
		results = append(results, userEvent)
	}

	return results, nil
}

// ListProjects returns a list of projects accessible by the authenticated user.
func (g *GitLabProvider) ListProjects() ([]Project, error) {
	opts := &gitlab.ListProjectsOptions{
		Membership: gitlab.Ptr(true),
		Simple:     gitlab.Ptr(true),
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	}

	projects, _, err := g.client.Projects.ListProjects(opts)
	if err != nil {
		return nil, fmt.Errorf("git: ListProjects: %w", err)
	}

	var results []Project
	for _, p := range projects {
		results = append(results, Project{
			ID:   int64(p.ID),
			Name: p.NameWithNamespace,
		})
	}

	return results, nil
}
