package gitlab

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// GetMRJobs returns all CI/CD jobs from the latest pipeline of the given MR.
func (c *Client) GetMRJobs(mrID int64) ([]Job, error) {
	// Step 1: resolve the MR's head pipeline ID.
	// GetMergeRequestsOptions → replaced by GetMergeRequestOptions (deprecated in v0.107).
	mr, _, err := c.glc.MergeRequests.GetMergeRequest(c.projectID, mrID, &gitlab.GetMergeRequestsOptions{})
	if err != nil {
		return nil, fmt.Errorf("gitlab: GetMRJobs — fetch MR %d: %w", mrID, err)
	}
	if mr.HeadPipeline == nil {
		return nil, fmt.Errorf("gitlab: GetMRJobs — MR %d has no pipeline", mrID)
	}

	// Step 2: list all jobs in that pipeline.
	// ListJobsOptions.Scope is now a []BuildStateValue, not a string.
	opts := &gitlab.ListJobsOptions{
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}
	glJobs, _, err := c.glc.Jobs.ListPipelineJobs(c.projectID, mr.HeadPipeline.ID, opts)
	if err != nil {
		return nil, fmt.Errorf("gitlab: GetMRJobs — list pipeline %d jobs: %w", mr.HeadPipeline.ID, err)
	}

	jobs := make([]Job, 0, len(glJobs))
	for _, j := range glJobs {
		jobs = append(jobs, mapJob(j))
	}
	return jobs, nil
}

// GetJob fetches a single CI/CD job by its job ID.
func (c *Client) GetJob(mrID, jobID int64) (*Job, error) {
	j, _, err := c.glc.Jobs.GetJob(c.projectID, jobID)
	if err != nil {
		return nil, fmt.Errorf("gitlab: GetJob (MR %d, job %d): %w", mrID, jobID, err)
	}
	result := mapJob(j)
	return &result, nil
}

// mapJob converts a go-gitlab Job struct into our local Job type.
func mapJob(j *gitlab.Job) Job {
	job := Job{
		ID:       j.ID,
		Name:     j.Name,
		Status:   j.Status,
		Stage:    j.Stage,
		Duration: j.Duration,
		WebURL:   j.WebURL,
	}
	if j.StartedAt != nil {
		job.StartedAt = j.StartedAt
	}
	if j.FinishedAt != nil {
		job.FinishedAt = j.FinishedAt
	}
	if j.Pipeline.ID != 0 {
		job.PipelineID = j.Pipeline.ID
	}
	return job
}
