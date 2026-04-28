package auth

import (
	"errors"
	"fmt"
	"pr-reviewer-ai/internal/repository"
	"strings"
)

// AuthService manages user sessions and secure token storage.
// It depends on repository.TokenRepository for persistence and an AES-GCM
// encrypt/decrypt pair for encryption at rest.
type AuthService struct {
	repo    repository.TokenRepository
	encrypt func(plaintext string) (string, error)
	decrypt func(ciphertext string) (string, error)
}

// NewAuthService creates an AuthService.
//
//	repo      – concrete TokenRepository (Postgres, in-memory mock, etc.)
//	encrypt   – encrypts a plaintext token; typically crypto.Encrypt bound to a key
//	decrypt   – decrypts a stored ciphertext; typically crypto.Decrypt bound to a key
func NewAuthService(
	repo repository.TokenRepository,
	encrypt func(string) (string, error),
	decrypt func(string) (string, error),
) *AuthService {
	return &AuthService{repo: repo, encrypt: encrypt, decrypt: decrypt}
}

// Login validates, encrypts, and persists the Personal Access Token for userID.
func (a *AuthService) Login(userID, rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return errors.New("auth: token must not be empty")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("auth: user_id must not be empty")
	}

	encrypted, err := a.encrypt(rawToken)
	if err != nil {
		return fmt.Errorf("auth: encryption failed: %w", err)
	}

	if err := a.repo.StoreToken(userID, encrypted); err != nil {
		return fmt.Errorf("auth: failed to store token: %w", err)
	}
	return nil
}

// Logout removes the stored token for userID.
func (a *AuthService) Logout(userID string) error {
	if err := a.repo.DeleteToken(userID); err != nil {
		return fmt.Errorf("auth: failed to delete token: %w", err)
	}
	return nil
}

// GetToken retrieves and decrypts the stored token for userID.
func (a *AuthService) GetToken(userID string) (string, error) {
	enc, err := a.repo.GetToken(userID)
	if err != nil {
		return "", fmt.Errorf("auth: failed to load token: %w", err)
	}

	rawToken, err := a.decrypt(enc)
	if err != nil {
		return "", fmt.Errorf("auth: decryption failed: %w", err)
	}
	return rawToken, nil
}
