// Package postgres provides PostgreSQL implementations of the repository interfaces.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTokenNotFound is returned by GetToken when no token exists for the user.
var ErrTokenNotFound = errors.New("repository: token not found for user")

// TokenRepo is the Postgres implementation of repository.TokenRepository.
type TokenRepo struct {
	pool *pgxpool.Pool
}

// NewTokenRepo creates a TokenRepo backed by the provided connection pool.
func NewTokenRepo(pool *pgxpool.Pool) *TokenRepo {
	return &TokenRepo{pool: pool}
}

// StoreToken upserts the encrypted token for userID.
// Creates the user row first if it does not exist.
func (r *TokenRepo) StoreToken(userID, encryptedToken string) error {
	ctx := context.Background()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Ensure user exists.
	_, err = tx.Exec(ctx,
		`INSERT INTO mr_reviewer_app.users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("repository: upsert user: %w", err)
	}

	// Upsert token.
	_, err = tx.Exec(ctx, `
		INSERT INTO mr_reviewer_app.user_tokens (user_id, encrypted_token, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE
		    SET encrypted_token = EXCLUDED.encrypted_token,
		        updated_at      = NOW()
	`, userID, encryptedToken)
	if err != nil {
		return fmt.Errorf("repository: upsert token: %w", err)
	}

	return tx.Commit(ctx)
}

// GetToken retrieves the encrypted token for userID.
func (r *TokenRepo) GetToken(userID string) (string, error) {
	var enc string
	err := r.pool.QueryRow(context.Background(),
		`SELECT encrypted_token FROM mr_reviewer_app.user_tokens WHERE user_id = $1`,
		userID,
	).Scan(&enc)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("repository: get token: %w", err)
	}
	return enc, nil
}

// DeleteToken removes the stored token for userID.
func (r *TokenRepo) DeleteToken(userID string) error {
	_, err := r.pool.Exec(context.Background(),
		`DELETE FROM mr_reviewer_app.user_tokens WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("repository: delete token: %w", err)
	}
	return nil
}
