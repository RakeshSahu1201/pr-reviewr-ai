// Package gitlab provides a dedicated GitLab API client with rich methods.
// It is intentionally separate from internal/git (which only satisfies the
// GitProvider interface). Use this package for any direct GitLab interaction
// beyond what the reviewer workflow requires.
package gitlab

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// Client wraps the go-gitlab SDK and exposes project, MR, diff, and job methods.
type Client struct {
	glc       *gitlab.Client
	projectID string // default project — can be overridden per-call
}

// New creates a GitLab Client.
//
//	baseURL   – GitLab instance URL (e.g. "https://gitlab.com")
//	token     – Personal Access Token with api scope
//	projectID – default project ID or "namespace/project" path
func New(baseURL, token, projectID string) (*Client, error) {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to create client: %w", err)
	}
	return &Client{glc: client, projectID: projectID}, nil
}
