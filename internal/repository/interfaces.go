// Package repository defines the data-access interfaces for the PR Reviewer service.
// No SQL, no ORM, no driver import. Concrete implementations live in sub-packages.
package repository

import (
	"pr-reviewer-ai/ent"
	"time"
)

// TokenRepository manages user credentials and encrypted GitLab Personal Access Tokens.
// Business logic (AuthService) depends only on this interface.
type TokenRepository interface {
	// RegisterUser creates a new user with their password and gitlab details, and upserts their token.
	RegisterUser(username, passwordHash, encryptedToken, encryptedWebUrl string, gitlabUserId int) error

	// StoreToken upserts an encrypted token for the user.
	// It also creates the user row if one does not exist.
	StoreToken(userID int64, encryptedToken string) error

	// GetToken retrieves the encrypted token for the user.
	// Returns ErrTokenNotFound if no token has been stored.
	GetToken(userID int64) (string, error)

	// LoginUser validates the user credentials and returns the user info.
	LoginUser(username, password string) (*ent.User, error)

	// GetWebUrl retrieves the encrypted GitLab base URL for the user.
	GetWebUrl(userID int64) (string, error)

	// UpdateProjectID updates the default GitLab project ID for the user.
	UpdateProjectID(userID int64, projectID int64) error

	// GetProjectID retrieves the default GitLab project ID for the user.
	GetProjectID(userID int64) (int64, error)

	// GetGitlabUserID retrieves the GitLab numeric user ID for the given username.
	GetGitlabUserID(username string) (int, error)

	// GetAllTokens retrieves all user tokens for the background worker.
	GetAllTokens() ([]UserTokenInfo, error)

	// UpdateLastEventID updates the last processed event ID for the user.
	UpdateLastEventID(userID int64, eventID int64) error
}

type UserTokenInfo struct {
	UserID      int64
	Token       string
	WebUrl      string
	ProjectID   int64
	LastEventID int64
}

// ReviewLog is a single audit record returned by ReviewLogRepository.
type ReviewLog struct {
	ID         int64
	UserID     int64
	MRID       int
	ProjectID  string
	Comment    string
	ReviewedAt time.Time
}

// ReviewLogRepository persists and retrieves MR review audit records.
type ReviewLogRepository interface {
	// LogReview records a completed review.
	LogReview(userID int64, mrID int, projectID, comment string) error

	// ListReviews returns all review logs for the given user, newest first.
	ListReviews(userID int64) ([]ReviewLog, error)
}
