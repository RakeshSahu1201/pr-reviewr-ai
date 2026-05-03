package postgres

import (
	"context"
	"fmt"

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
func (r *ReviewLogRepo) LogReview(userID int64, mrID int, projectID, comment string) error {
	uid := int(userID)
	ctx := context.Background()

	err := r.client.ReviewLog.Create().
		SetOwnerID(uid).
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
func (r *ReviewLogRepo) ListReviews(userID int64) ([]repository.ReviewLog, error) {
	uid := int(userID)

	ctx := context.Background()
	rows, err := r.client.ReviewLog.Query().
		Where(reviewlog.HasOwnerWith(user.IDEQ(uid))).
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
