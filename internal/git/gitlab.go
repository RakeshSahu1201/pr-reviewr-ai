package git

import (
	"fmt"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// GitLabProvider implements GitProvider using the GitLab REST API.
// It is the only file in the entire codebase that imports a GitLab-specific SDK.
type GitLabProvider struct {
	client    *gitlab.Client
	projectID string
}

// NewGitLabProvider constructs a GitLabProvider.
//
//	baseURL   – GitLab instance URL, e.g. "https://gitlab.com"
//	token     – Personal Access Token with api scope
//	projectID – GitLab project ID or namespace/project path
func NewGitLabProvider(baseURL, token, projectID string) (*GitLabProvider, error) {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("git: failed to create GitLab client: %w", err)
	}
	return &GitLabProvider{client: client, projectID: projectID}, nil
}

// FetchDiff retrieves the unified diff for the given merge request.
// Uses ListMergeRequestDiffs (replaces deprecated GetMergeRequestChanges).
func (g *GitLabProvider) FetchDiff(mrID int) (string, error) {
	opts := &gitlab.ListMergeRequestDiffsOptions{
		ListOptions: gitlab.ListOptions{PerPage: 50},
	}

	var sb strings.Builder
	for {
		diffs, resp, err := g.client.MergeRequests.ListMergeRequestDiffs(g.projectID, int64(mrID), opts)
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
		opts.Page = resp.NextPage
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

// FetchRecentEvents fetches the events performed by the authenticated user since the given event ID.
// It uses ListCurrentUserContributionEvents to get only the relevant UI actions.
func (g *GitLabProvider) FetchRecentEvents(sinceEventID int) ([]UserEvent, error) {
	opts := &gitlab.ListContributionEventsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 20,
			Page:    1,
		},
	}

	events, _, err := g.client.Events.ListCurrentUserContributionEvents(opts)
	if err != nil {
		return nil, fmt.Errorf("git: FetchRecentEvents: %w", err)
	}

	var results []UserEvent
	// Build backward so we process the oldest "new" events first implicitly.
	// Actually we reverse the array directly.
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if int(event.ID) <= sinceEventID {
			continue
		}

		userEvent := UserEvent{
			ID:          event.ID,
			ActionName:  event.ActionName,
			TargetType:  event.TargetType,
			TargetTitle: event.TargetTitle,
		}

		if event.Note != nil {
			userEvent.Body = event.Note.Body
		}

		// PushData is a struct, so we can just map the Ref directly (it defaults to empty string)
		userEvent.Ref = event.PushData.Ref

		results = append(results, userEvent)
	}

	return results, nil
}
