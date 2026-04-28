package crypto_test

import (
	"crypto/rand"
	"pr-reviewer-ai/internal/crypto"
	"strings"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := "glpat-super-secret-token"
	encrypted, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}

	decrypted, err := crypto.Decrypt(key, encrypted)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("round-trip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncrypt_ProducesUniqueCiphertexts(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	c1, _ := crypto.Encrypt(key, "same-input")
	c2, _ := crypto.Encrypt(key, "same-input")

	if c1 == c2 {
		t.Error("Encrypt must produce unique ciphertexts (nonce randomness)")
	}
}

func TestDecrypt_WrongKey_ReturnsError(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	enc, _ := crypto.Encrypt(key1, "secret")
	if _, err := crypto.Decrypt(key2, enc); err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestEncrypt_ShortKey_ReturnsError(t *testing.T) {
	_, err := crypto.Encrypt([]byte("short"), "hello")
	if err == nil {
		t.Error("expected error for key shorter than 32 bytes")
	}
}

func TestEncrypt_OutputIsBase64(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	out, _ := crypto.Encrypt(key, "test")
	// base64url has no '+' or '/'
	if strings.ContainsAny(out, "+/") {
		t.Errorf("expected base64url (no +/), got: %s", out)
	}
}
