package gitlab

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// ListProjects returns all projects visible to the authenticated user.
// Results are ordered by last activity (most recent first).
func (c *Client) ListProjects() ([]Project, error) {
	opts := &gitlab.ListProjectsOptions{
		OrderBy:    gitlab.Ptr("last_activity_at"),
		Sort:       gitlab.Ptr("desc"),
		Membership: gitlab.Ptr(true), // only projects the user is a member of
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	var all []Project
	for {
		projects, resp, err := c.glc.Projects.ListProjects(opts)
		if err != nil {
			return nil, fmt.Errorf("gitlab: ListProjects: %w", err)
		}

		for _, p := range projects {
			all = append(all, Project{
				ID:                p.ID,
				Name:              p.Name,
				PathWithNamespace: p.PathWithNamespace,
				Description:       p.Description,
				Visibility:        string(p.Visibility),
				WebURL:            p.WebURL,
				CreatedAt:         p.CreatedAt,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return all, nil
}
