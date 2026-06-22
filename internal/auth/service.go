package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"pr-reviewer-ai/ent"
	"pr-reviewer-ai/internal/repository"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
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
func NewAuthService(
	repo repository.TokenRepository,
	encrypt func(string) (string, error),
	decrypt func(string) (string, error),
) *AuthService {
	return &AuthService{repo: repo, encrypt: encrypt, decrypt: decrypt}
}

// validateGitlabToken calls the GitLab Profile API to ensure the token is valid.
func validateGitlabToken(webUrl, token string) (int, error) {
	reqURL := fmt.Sprintf("%s/api/v4/user", webUrl)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GitLab API returned status: %d", resp.StatusCode)
	}

	var data struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("failed to decode GitLab response: %w", err)
	}

	return data.ID, nil
}

// Register validates the gitlab credentials, hashes the password, and stores the user.
func (a *AuthService) Register(username, password, token, webUrl string) (int, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	token = strings.TrimSpace(token)
	webUrl = strings.TrimRight(strings.TrimSpace(webUrl), "/")

	if username == "" || password == "" || token == "" || webUrl == "" {
		return 0, errors.New("auth: username, password, token, and webUrl are required")
	}

	gitlabUserID, err := validateGitlabToken(webUrl, token)
	if err != nil {
		return 0, fmt.Errorf("auth: invalid GitLab credentials or token: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("auth: failed to hash password: %w", err)
	}

	encryptedToken, err := a.encrypt(token)
	if err != nil {
		return 0, fmt.Errorf("auth: encryption failed: %w", err)
	}
	encryptedWebUrl, err := a.encrypt(webUrl)
	if err != nil {
		return 0, fmt.Errorf("auth: encryption failed: %w", err)
	}

	if err := a.repo.RegisterUser(username, string(hash), encryptedToken, encryptedWebUrl, gitlabUserID); err != nil {
		return 0, fmt.Errorf("auth: failed to register user: %w", err)
	}

	return gitlabUserID, nil
}

// Login validates credentials and returns the user object.
func (a *AuthService) Login(username, password string) (*ent.User, error) {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return nil, errors.New("auth: username and password must not be empty")
	}

	user, err := a.repo.LoginUser(username, password)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to login user: %w", err)
	}
	return user, nil
}

// StoreToken validates, encrypts, and persists the Personal Access Token for userID.
func (a *AuthService) StoreToken(userID int64, rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return errors.New("auth: token must not be empty")
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

// GetToken retrieves and decrypts the stored token for userID.
func (a *AuthService) GetToken(userID int64) (string, error) {
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

// GetWebUrl retrieves and decrypts the stored GitLab base URL for userID.
func (a *AuthService) GetWebUrl(userID int64) (string, error) {
	enc, err := a.repo.GetWebUrl(userID)
	if err != nil {
		return "", fmt.Errorf("auth: failed to load webUrl: %w", err)
	}

	rawUrl, err := a.decrypt(enc)
	if err != nil {
		return "", fmt.Errorf("auth: decryption failed: %w", err)
	}
	return rawUrl, nil
}

// GetGitlabUserID returns the stored GitLab numeric user ID for the given username.
func (a *AuthService) GetGitlabUserID(username string) (int, error) {
	id, err := a.repo.GetGitlabUserID(username)
	if err != nil {
		return 0, fmt.Errorf("auth: %w", err)
	}
	return id, nil
}

// GetGitlabUserIDByID returns the stored GitLab numeric user ID for the given internal userID.
func (a *AuthService) GetGitlabUserIDByID(userID int64) (int, error) {
	id, err := a.repo.GetGitlabUserIDByID(userID)
	if err != nil {
		return 0, fmt.Errorf("auth: %w", err)
	}
	return id, nil
}

// UpdateProjectID updates the default GitLab project ID for the user.
func (a *AuthService) UpdateProjectID(userID int64, projectID int64) error {
	if projectID <= 0 {
		return errors.New("auth: project_id must be positive")
	}
	if err := a.repo.UpdateProjectID(userID, projectID); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	return nil
}

// GetProjectID retrieves the default GitLab project ID for the user.
func (a *AuthService) GetProjectID(userID int64) (int64, error) {
	id, err := a.repo.GetProjectID(userID)
	if err != nil {
		return 0, fmt.Errorf("auth: %w", err)
	}
	return id, nil
}

type UserTokenInfo struct {
	UserID       int64
	Token        string
	WebUrl       string
	ProjectID    int64
	GitlabUserID int
}

// GetAllUserTokens retrieves all tokens and decrypts them.
func (a *AuthService) GetAllUserTokens() ([]UserTokenInfo, error) {
	infos, err := a.repo.GetAllTokens()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	var results []UserTokenInfo
	for _, info := range infos {
		tok, errT := a.decrypt(info.Token)
		url, errU := a.decrypt(info.WebUrl)
		if errT != nil || errU != nil {
			continue // skip malformed
		}
		results = append(results, UserTokenInfo{
			UserID:       info.UserID,
			Token:        tok,
			WebUrl:       url,
			ProjectID:    info.ProjectID,
			GitlabUserID: info.GitlabUserID,
		})
	}
	return results, nil
}

