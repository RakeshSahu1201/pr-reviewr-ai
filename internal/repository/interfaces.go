// Package repository defines the data-access interfaces for the PR Reviewer service.
// No SQL, no ORM, no driver import. Concrete implementations live in sub-packages.
package repository

import "time"

// TokenRepository manages user credentials and encrypted GitLab Personal Access Tokens.
// Business logic (AuthService) depends only on this interface.
type TokenRepository interface {
	// RegisterUser creates a new user with their password and gitlab details, and upserts their token.
	RegisterUser(username, passwordHash, encryptedToken, encryptedWebUrl string, gitlabUserId int) error

	// StoreToken upserts an encrypted token for the user.
	// It also creates the user row if one does not exist.
	StoreToken(userID string, encryptedToken string) error

	// GetToken retrieves the encrypted token for the user.
	// Returns ErrTokenNotFound if no token has been stored.
	GetToken(userID string) (string, error)

	// DeleteToken removes the user's stored token.
	DeleteToken(userID string) error

	// LoginUser validates the user credentials and returns the user info.
	LoginUser(username, password string) error

	// GetWebUrl retrieves the encrypted GitLab base URL for the user.
	GetWebUrl(userID string) (string, error)

	// GetGitlabUserID retrieves the GitLab numeric user ID for the given username.
	GetGitlabUserID(username string) (int, error)
}

// ReviewLog is a single audit record returned by ReviewLogRepository.
type ReviewLog struct {
	ID         int64
	UserID     string
	MRID       int
	ProjectID  string
	Comment    string
	ReviewedAt time.Time
}

// ReviewLogRepository persists and retrieves MR review audit records.
type ReviewLogRepository interface {
	// LogReview records a completed review.
	LogReview(userID string, mrID int, projectID, comment string) error

	// ListReviews returns all review logs for the given user, newest first.
	ListReviews(userID string) ([]ReviewLog, error)
}
