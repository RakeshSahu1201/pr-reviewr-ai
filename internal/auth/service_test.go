package auth_test

import (
	"errors"
	"pr-reviewer-ai/internal/auth"
	"testing"
)

// --- mockTokenRepository ---

type mockTokenRepo struct {
	data map[string]string
}

func newMockTokenRepo() *mockTokenRepo {
	return &mockTokenRepo{data: make(map[string]string)}
}

func (m *mockTokenRepo) StoreToken(userID, enc string) error {
	m.data[userID] = enc
	return nil
}

func (m *mockTokenRepo) GetToken(userID string) (string, error) {
	v, ok := m.data[userID]
	if !ok {
		return "", errors.New("token not found")
	}
	return v, nil
}

func (m *mockTokenRepo) DeleteToken(userID string) error {
	delete(m.data, userID)
	return nil
}

// --- no-op encrypt/decrypt (identity functions for tests) ---

func identityEncrypt(s string) (string, error) { return "enc:" + s, nil }
func identityDecrypt(s string) (string, error) {
	if len(s) < 4 {
		return "", errors.New("bad ciphertext")
	}
	return s[4:], nil // strip "enc:" prefix
}

// --- Tests ---

func TestLogin_StoresToken(t *testing.T) {
	repo := newMockTokenRepo()
	svc := auth.NewAuthService(repo, identityEncrypt, identityDecrypt)

	if err := svc.Login("rakesh", "glpat-test-token"); err != nil {
		t.Fatalf("Login error: %v", err)
	}

	token, err := svc.GetToken("rakesh")
	if err != nil {
		t.Fatalf("GetToken error: %v", err)
	}
	if token != "glpat-test-token" {
		t.Errorf("expected %q, got %q", "glpat-test-token", token)
	}
}

func TestLogin_RejectsEmptyToken(t *testing.T) {
	svc := auth.NewAuthService(newMockTokenRepo(), identityEncrypt, identityDecrypt)
	if err := svc.Login("rakesh", "   "); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestLogin_RejectsEmptyUserID(t *testing.T) {
	svc := auth.NewAuthService(newMockTokenRepo(), identityEncrypt, identityDecrypt)
	if err := svc.Login("", "glpat-token"); err == nil {
		t.Fatal("expected error for empty user_id")
	}
}

func TestLogin_TrimsWhitespace(t *testing.T) {
	repo := newMockTokenRepo()
	svc := auth.NewAuthService(repo, identityEncrypt, identityDecrypt)

	_ = svc.Login("rakesh", "  glpat-trimmed  ")
	token, _ := svc.GetToken("rakesh")
	if token != "glpat-trimmed" {
		t.Errorf("expected trimmed token, got %q", token)
	}
}

func TestLogout_RemovesToken(t *testing.T) {
	repo := newMockTokenRepo()
	svc := auth.NewAuthService(repo, identityEncrypt, identityDecrypt)

	_ = svc.Login("rakesh", "glpat-token")
	if err := svc.Logout("rakesh"); err != nil {
		t.Fatalf("Logout error: %v", err)
	}

	if _, err := svc.GetToken("rakesh"); err == nil {
		t.Error("expected error after logout, got nil")
	}
}

func TestGetToken_ErrorWhenNotSet(t *testing.T) {
	svc := auth.NewAuthService(newMockTokenRepo(), identityEncrypt, identityDecrypt)
	if _, err := svc.GetToken("nobody"); err == nil {
		t.Error("expected error for unknown user")
	}
}
