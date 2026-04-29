// Package postgres provides PostgreSQL implementations of the repository interfaces using ent.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"pr-reviewer-ai/ent"
	"pr-reviewer-ai/ent/user"
	"pr-reviewer-ai/ent/usertoken"

	"golang.org/x/crypto/bcrypt"
)

// ErrTokenNotFound is returned by GetToken when no token exists for the user.
var ErrTokenNotFound = errors.New("repository: token not found for user")

// TokenRepo is the ent-backed implementation of repository.TokenRepository.
type TokenRepo struct {
	client *ent.Client
}

// NewTokenRepo creates a TokenRepo backed by the provided ent client.
func NewTokenRepo(client *ent.Client) *TokenRepo {
	return &TokenRepo{client: client}
}

// RegisterUser creates or updates a user with their password and GitLab details,
// then upserts their encrypted token — all within a single transaction.
func (r *TokenRepo) RegisterUser(username, passwordHash, encryptedToken, encryptedWebUrl string, gitlabUserId int) error {
	ctx := context.Background()

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Upsert user: find by username.
	u, err := tx.User.Query().Where(user.Username(username)).Only(ctx)
	if ent.IsNotFound(err) {
		u, err = tx.User.Create().
			SetUsername(username).
			SetPassword(passwordHash).
			SetGitlabUserID(gitlabUserId).
			Save(ctx)
	} else if err == nil {
		u, err = u.Update().
			SetPassword(passwordHash).
			SetGitlabUserID(gitlabUserId).
			Save(ctx)
	}
	if err != nil {
		return fmt.Errorf("repository: upsert user: %w", err)
	}

	// Upsert token.
	if err := upsertToken(ctx, tx, u.ID, encryptedToken, encryptedWebUrl); err != nil {
		return err
	}

	return tx.Commit()
}

// LoginUser validates the user's credentials using bcrypt.
func (r *TokenRepo) LoginUser(username, password string) error {
	u, err := r.client.User.Query().Where(user.Username(username)).Only(context.Background())
	if ent.IsNotFound(err) {
		return fmt.Errorf("repository: user not found: %s", username)
	}
	if err != nil {
		return fmt.Errorf("repository: get user: %w", err)
	}
	if u.Password == nil {
		return fmt.Errorf("repository: password not set for user: %s", username)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*u.Password), []byte(password)); err != nil {
		return fmt.Errorf("repository: invalid password")
	}
	return nil
}

// GetWebUrl retrieves the encrypted GitLab base URL for userID (which is stringified gitlab_user_id).
func (r *TokenRepo) GetWebUrl(userID string) (string, error) {
	gid, err := strconv.Atoi(userID)
	if err != nil {
		return "", fmt.Errorf("repository: invalid user id: %s", userID)
	}

	tok, err := r.client.UserToken.Query().
		Where(usertoken.HasOwnerWith(user.GitlabUserIDEQ(gid))).
		Only(context.Background())
	if ent.IsNotFound(err) {
		return "", fmt.Errorf("repository: token not found for gitlab user id: %d", gid)
	}
	if err != nil {
		return "", fmt.Errorf("repository: get web url: %w", err)
	}
	if tok.WebURL == nil {
		return "", fmt.Errorf("repository: web_url not set for gitlab user id: %d", gid)
	}
	return *tok.WebURL, nil
}

// StoreToken upserts the encrypted token for userID (which is stringified gitlab_user_id).
func (r *TokenRepo) StoreToken(userID string, encryptedToken string) error {
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

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := upsertToken(ctx, tx, u.ID, encryptedToken, ""); err != nil {
		return err
	}

	return tx.Commit()
}

// GetToken retrieves the encrypted token for userID (which is stringified gitlab_user_id).
func (r *TokenRepo) GetToken(userID string) (string, error) {
	gid, err := strconv.Atoi(userID)
	if err != nil {
		return "", fmt.Errorf("repository: invalid user id: %s", userID)
	}

	ctx := context.Background()
	tok, err := r.client.UserToken.Query().
		Where(usertoken.HasOwnerWith(user.GitlabUserIDEQ(gid))).
		Only(ctx)

	if ent.IsNotFound(err) {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("repository: get token: %w", err)
	}
	return tok.Token, nil
}

// DeleteToken removes the stored token for userID (which is stringified gitlab_user_id).
func (r *TokenRepo) DeleteToken(userID string) error {
	gid, err := strconv.Atoi(userID)
	if err != nil {
		return fmt.Errorf("repository: invalid user id: %s", userID)
	}

	ctx := context.Background()
	_, err = r.client.UserToken.Delete().
		Where(usertoken.HasOwnerWith(user.GitlabUserIDEQ(gid))).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("repository: delete token: %w", err)
	}
	return nil
}

// upsertToken creates or updates the UserToken for internal ownerID within the given transaction.
func upsertToken(ctx context.Context, tx *ent.Tx, ownerID int, encryptedToken, encryptedWebUrl string) error {
	existing, err := tx.UserToken.Query().
		Where(usertoken.HasOwnerWith(user.IDEQ(ownerID))).
		Only(ctx)

	if ent.IsNotFound(err) {
		create := tx.UserToken.Create().
			SetOwnerID(ownerID).
			SetToken(encryptedToken)
		if encryptedWebUrl != "" {
			create.SetWebURL(encryptedWebUrl)
		}
		err = create.Exec(ctx)
	} else if err == nil {
		update := tx.UserToken.UpdateOne(existing).
			SetToken(encryptedToken)
		if encryptedWebUrl != "" {
			update.SetWebURL(encryptedWebUrl)
		}
		err = update.Exec(ctx)
	}
	if err != nil {
		return fmt.Errorf("repository: upsert token: %w", err)
	}
	return nil
}

// GetGitlabUserID retrieves the stored GitLab numeric user ID for the given username.
func (r *TokenRepo) GetGitlabUserID(username string) (int, error) {
	u, err := r.client.User.Query().Where(user.Username(username)).Only(context.Background())
	if ent.IsNotFound(err) {
		return 0, fmt.Errorf("repository: user not found: %s", username)
	}
	if err != nil {
		return 0, fmt.Errorf("repository: get gitlab user id: %w", err)
	}
	if u.GitlabUserID == nil {
		return 0, fmt.Errorf("repository: gitlab_user_id not set for user: %s", username)
	}
	return *u.GitlabUserID, nil
}

