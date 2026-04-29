package postgres

import (
	"context"
	"fmt"
	"strconv"

	"pr-reviewer-ai/ent"
	"pr-reviewer-ai/ent/reviewlog"
	"pr-reviewer-ai/ent/user"
	"pr-reviewer-ai/internal/repository"
)

// ReviewLogRepo is the ent-backed implementation of repository.ReviewLogRepository.
type ReviewLogRepo struct {
	client *ent.Client
}

// NewReviewLogRepo creates a ReviewLogRepo backed by the provided ent client.
func NewReviewLogRepo(client *ent.Client) *ReviewLogRepo {
	return &ReviewLogRepo{client: client}
}

// LogReview inserts a new review audit record.
func (r *ReviewLogRepo) LogReview(userID string, mrID int, projectID, comment string) error {
	gid, err := strconv.Atoi(userID)
	if err != nil {
		return fmt.Errorf("repository: invalid user id: %s", userID)
	}

	ctx := context.Background()
	u, err := r.client.User.Query().Where(user.GitlabUserIDEQ(gid)).Only(ctx)
	if ent.IsNotFound(err) {
		return fmt.Errorf("repository: user not found with gitlab id: %d", gid)
	}
	if err != nil {
		return fmt.Errorf("repository: get user: %w", err)
	}

	err = r.client.ReviewLog.Create().
		SetOwner(u).
		SetMrID(mrID).
		SetProjectID(projectID).
		SetComment(comment).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("repository: log review: %w", err)
	}
	return nil
}

// ListReviews returns all review logs for userID, newest first.
func (r *ReviewLogRepo) ListReviews(userID string) ([]repository.ReviewLog, error) {
	gid, err := strconv.Atoi(userID)
	if err != nil {
		return nil, fmt.Errorf("repository: invalid user id: %s", userID)
	}

	ctx := context.Background()
	rows, err := r.client.ReviewLog.Query().
		Where(reviewlog.HasOwnerWith(user.GitlabUserIDEQ(gid))).
		Order(ent.Desc(reviewlog.FieldReviewedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository: list reviews: %w", err)
	}

	logs := make([]repository.ReviewLog, 0, len(rows))
	for _, row := range rows {
		logs = append(logs, repository.ReviewLog{
			ID:         int64(row.ID),
			UserID:     userID,
			MRID:       row.MrID,
			ProjectID:  row.ProjectID,
			Comment:    row.Comment,
			ReviewedAt: row.ReviewedAt,
		})
	}
	return logs, nil
}
