package gitlab

import (
	"fmt"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// GetMR fetches a single Merge Request by its project-scoped IID (the !123 number).
func (c *Client) GetMR(mrID int64) (*MergeRequest, error) {
	// GetMergeRequestsOptions was deprecated in v0.107.0 — use GetMergeRequestOptions.
	mr, _, err := c.glc.MergeRequests.GetMergeRequest(c.projectID, mrID, &gitlab.GetMergeRequestsOptions{})
	if err != nil {
		return nil, fmt.Errorf("gitlab: GetMR %d: %w", mrID, err)
	}

	authorName := ""
	if mr.Author != nil {
		authorName = mr.Author.Name
	}

	return &MergeRequest{
		ID:           mr.ID,
		IID:          mr.IID,
		Title:        mr.Title,
		Description:  mr.Description,
		State:        mr.State,
		SourceBranch: mr.SourceBranch,
		TargetBranch: mr.TargetBranch,
		AuthorName:   authorName,
		WebURL:       mr.WebURL,
		CreatedAt:    mr.CreatedAt,
		UpdatedAt:    mr.UpdatedAt,
	}, nil
}

// GetDiff returns the file-level diffs for the given MR.
// Uses ListMergeRequestDiffs (replaces deprecated GetMergeRequestChanges, removed in GitLab 15.8+).
func (c *Client) GetDiff(mrID int64) ([]FileDiff, error) {
	opts := &gitlab.ListMergeRequestDiffsOptions{
		ListOptions: gitlab.ListOptions{PerPage: 50},
	}

	var diffs []FileDiff
	for {
		diffFiles, resp, err := c.glc.MergeRequests.ListMergeRequestDiffs(c.projectID, mrID, opts)
		if err != nil {
			return nil, fmt.Errorf("gitlab: GetDiff MR %d: %w", mrID, err)
		}
		for _, d := range diffFiles {
			diffs = append(diffs, FileDiff{
				OldPath:       d.OldPath,
				NewPath:       d.NewPath,
				Diff:          d.Diff,
				IsNewFile:     d.NewFile,
				IsRenamedFile: d.RenamedFile,
				IsDeletedFile: d.DeletedFile,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return diffs, nil
}

// GetDiffUnified returns the full diff for an MR as a single concatenated
// unified diff string — convenience wrapper over GetDiff.
func (c *Client) GetDiffUnified(mrID int64) (string, error) {
	diffs, err := c.GetDiff(mrID)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, d := range diffs {
		sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", d.OldPath, d.NewPath))
		sb.WriteString(d.Diff)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
