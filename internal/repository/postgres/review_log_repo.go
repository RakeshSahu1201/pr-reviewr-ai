package postgres

import (
	"context"
	"fmt"
	"pr-reviewer-ai/internal/repository"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReviewLogRepo is the Postgres implementation of repository.ReviewLogRepository.
type ReviewLogRepo struct {
	pool *pgxpool.Pool
}

// NewReviewLogRepo creates a ReviewLogRepo backed by the provided pool.
func NewReviewLogRepo(pool *pgxpool.Pool) *ReviewLogRepo {
	return &ReviewLogRepo{pool: pool}
}

// LogReview inserts a new review audit record.
func (r *ReviewLogRepo) LogReview(userID string, mrID int, projectID, comment string) error {
	_, err := r.pool.Exec(context.Background(), `
		INSERT INTO mr_reviewer_app.review_logs (user_id, mr_id, project_id, comment)
		VALUES ($1, $2, $3, $4)
	`, userID, mrID, projectID, comment)
	if err != nil {
		return fmt.Errorf("repository: log review: %w", err)
	}
	return nil
}

// ListReviews returns all review logs for userID, newest first.
func (r *ReviewLogRepo) ListReviews(userID string) ([]repository.ReviewLog, error) {
	rows, err := r.pool.Query(context.Background(), `
		SELECT id, user_id, mr_id, project_id, comment, reviewed_at
		FROM mr_reviewer_app.review_logs
		WHERE user_id = $1
		ORDER BY reviewed_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: list reviews: %w", err)
	}
	defer rows.Close()

	var logs []repository.ReviewLog
	for rows.Next() {
		var rl repository.ReviewLog
		if err := rows.Scan(&rl.ID, &rl.UserID, &rl.MRID, &rl.ProjectID, &rl.Comment, &rl.ReviewedAt); err != nil {
			return nil, fmt.Errorf("repository: scan review log: %w", err)
		}
		logs = append(logs, rl)
	}
	return logs, rows.Err()
}
