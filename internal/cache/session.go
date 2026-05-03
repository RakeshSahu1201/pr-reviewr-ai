// Package cache provides a Redis-backed session store for pr-reviewer-ai.
// Sensitive fields (Token, WebUrl) are encrypted with AES-256-GCM before
// being written to Redis and decrypted on retrieval.  All other cache
// operations fail-open: if Redis is unavailable the caller falls back to
// the Postgres repository.
package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ────────────────────────────────────────────────────────────────────────────
// Key taxonomy
// ────────────────────────────────────────────────────────────────────────────

const (
	sessionKeyPrefix = "cache:session:" // cache:session:{userID}
	sessionKeyTTL    = 24 * time.Hour   // default TTL — overridden by token expiry
)

func sessionKey(userID int64) string {
	return sessionKeyPrefix + strconv.FormatInt(userID, 10)
}

// ────────────────────────────────────────────────────────────────────────────
// SessionData — the value retrieved from Redis
// ────────────────────────────────────────────────────────────────────────────

// SessionData holds the fields cached per authenticated user.
// Token and WebUrl are stored encrypted; Username, UserID, and ProjectID in plain text.
type SessionData struct {
	UserID    int64
	Username  string
	ProjectID int64
	// Encrypted ciphertext (base64) — decrypted transparently by SessionStore.
	EncryptedToken  string
	EncryptedWebUrl string
}

// ────────────────────────────────────────────────────────────────────────────
// SessionStore
// ────────────────────────────────────────────────────────────────────────────

// SessionStore wraps a Redis client and provides encrypted session caching.
// Uses Redis Hashes to support partial updates.
type SessionStore struct {
	rdb     *redis.Client
	encrypt func(plaintext string) (string, error)
	decrypt func(ciphertext string) (string, error)
}

// NewSessionStore creates a SessionStore.
func NewSessionStore(
	rdb *redis.Client,
	encrypt func(string) (string, error),
	decrypt func(string) (string, error),
) *SessionStore {
	return &SessionStore{rdb: rdb, encrypt: encrypt, decrypt: decrypt}
}

// Set writes a full session to Redis using HMSET.
func (s *SessionStore) Set(
	ctx context.Context,
	userID int64,
	username string,
	projectID int64,
	rawToken, rawWebUrl string,
	ttl time.Duration,
) error {
	if ttl <= 0 {
		ttl = sessionKeyTTL
	}

	encToken, err := s.encrypt(rawToken)
	if err != nil {
		return fmt.Errorf("cache: encrypt token: %w", err)
	}
	encWebUrl, err := s.encrypt(rawWebUrl)
	if err != nil {
		return fmt.Errorf("cache: encrypt webUrl: %w", err)
	}

	key := sessionKey(userID)
	data := map[string]any{
		"user_id":           userID,
		"username":          username,
		"project_id":        projectID,
		"encrypted_token":   encToken,
		"encrypted_web_url": encWebUrl,
	}

	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, key, data)
	pipe.Expire(ctx, key, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("cache: redis HSET: %w", err)
	}
	return nil
}

// Get retrieves and decrypts a session from Redis.
func (s *SessionStore) Get(ctx context.Context, userID int64) (*SessionData, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	fields, err := s.rdb.HGetAll(ctx, sessionKey(userID)).Result()
	if err != nil {
		return nil, fmt.Errorf("cache: redis HGETALL: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil // cache miss
	}

	uid, _ := strconv.ParseInt(fields["user_id"], 10, 64)
	pid, _ := strconv.ParseInt(fields["project_id"], 10, 64)

	return &SessionData{
		UserID:          uid,
		Username:        fields["username"],
		ProjectID:       pid,
		EncryptedToken:  fields["encrypted_token"],
		EncryptedWebUrl: fields["encrypted_web_url"],
	}, nil
}

// UpdateProjectID partially updates only the projectID field in Redis.
func (s *SessionStore) UpdateProjectID(ctx context.Context, userID int64, projectID int64) error {
	key := sessionKey(userID)
	
	// Check if session exists first (we don't want to create an incomplete hash).
	exists, err := s.rdb.Exists(ctx, key).Result()
	if err != nil || exists == 0 {
		return nil // skip if not in cache
	}

	if err := s.rdb.HSet(ctx, key, "project_id", projectID).Err(); err != nil {
		return fmt.Errorf("cache: redis HSET project_id: %w", err)
	}
	return nil
}

// Token retrieves and decrypts only the token field.
func (s *SessionStore) Token(ctx context.Context, userID int64) (string, error) {
	data, err := s.Get(ctx, userID)
	if err != nil || data == nil {
		return "", err
	}
	return s.decrypt(data.EncryptedToken)
}

// WebUrl retrieves and decrypts only the webUrl field.
func (s *SessionStore) WebUrl(ctx context.Context, userID int64) (string, error) {
	data, err := s.Get(ctx, userID)
	if err != nil || data == nil {
		return "", err
	}
	return s.decrypt(data.EncryptedWebUrl)
}

// ProjectID retrieves the projectID field.
func (s *SessionStore) ProjectID(ctx context.Context, userID int64) (int64, error) {
	data, err := s.Get(ctx, userID)
	if err != nil || data == nil {
		return 0, err
	}
	return data.ProjectID, nil
}

// Invalidate removes the session from Redis.
func (s *SessionStore) Invalidate(ctx context.Context, userID int64) error {
	return s.rdb.Del(ctx, sessionKey(userID)).Err()
}
