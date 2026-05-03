// Package config centralises all environment variable parsing for pr-reviewer-ai.
// Call Load() once at startup; pass the resulting Config through the dependency chain.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration parsed from environment variables.
type Config struct {
	// Database
	DatabaseURL string

	// Encryption — AES-256-GCM key (32 bytes, supplied as 64 hex chars)
	EncryptionKey []byte

	// JWT
	JWTSecret      []byte
	JWTExpiryHours int

	// GitLab
	GitLabBaseURL string
	GitLabProject int64 // GITLAB_PROJECT_ID — default project for all users

	// LLM — legacy single-provider (kept for backward compat)
	GeminiAPIKey string
	LLMModel     string

	// LLM — multi-provider registry (failover order = slice order)
	// Set via comma-separated LLM_PROVIDER_ORDER env var.
	// Available names: gemini, groq, deepseek, mistral, cerebras
	LLMProviderOrder []string // e.g. ["gemini","groq","deepseek"]

	// Per-provider API keys
	GroqAPIKey     string
	DeepSeekAPIKey string
	MistralAPIKey  string
	CerebrasAPIKey string

	// LLM safety
	LLMSanitize     bool     // if true, all prompts are redacted before sending
	LLMCustomRedact []string // extra proprietary terms to strip

	// Redis
	RedisAddr     string // host:port, default localhost:6379
	RedisPassword string // optional
	RedisDB       int    // default 0

	// Server
	Port string
}

// Load reads environment variables and returns a validated Config.
// Returns an error if any required variable is missing or malformed.
func Load() (*Config, error) {
	// Required vars
	pgHost := getOrDefault("POSTGRES_HOST", "localhost")
	pgPort := getOrDefault("POSTGRES_PORT", "5432")
	pgUser := getOrDefault("POSTGRES_USER", "mr_reviewer_app")
	pgPass := getOrDefault("POSTGRES_PASSWORD", "pr_reviewer")
	pgDB := getOrDefault("POSTGRES_DB", "pr_reviewer")
	dbURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?search_path=mr_reviewer_app", pgUser, pgPass, pgHost, pgPort, pgDB)
	encKeyHex, err := require("ENCRYPTION_KEY")
	if err != nil {
		return nil, err
	}
	encKey, err := hex.DecodeString(encKeyHex)
	if err != nil || len(encKey) != 32 {
		return nil, fmt.Errorf("config: ENCRYPTION_KEY must be a 64-char hex string (32 bytes)")
	}

	jwtSecretStr, err := require("JWT_SECRET")
	if err != nil {
		return nil, err
	}

	// Optional vars with defaults
	jwtExpiryHours, _ := strconv.Atoi(getOrDefault("JWT_EXPIRY_HOURS", "24"))
	if jwtExpiryHours <= 0 {
		jwtExpiryHours = 24
	}

	// Multi-provider order
	providerOrder := splitCSV(os.Getenv("LLM_PROVIDER_ORDER"))
	if len(providerOrder) == 0 {
		providerOrder = []string{"gemini", "groq", "deepseek", "mistral", "cerebras"}
	}

	redisDB, _ := strconv.Atoi(getOrDefault("REDIS_DB", "0"))

	return &Config{
		DatabaseURL:    dbURL,
		EncryptionKey:  encKey,
		JWTSecret:      []byte(jwtSecretStr),
		JWTExpiryHours: jwtExpiryHours,
		GitLabBaseURL:  getOrDefault("GITLAB_BASE_URL", "https://gitlab.com"),
		GitLabProject:  parseGitlabProjectID(os.Getenv("GITLAB_PROJECT_ID")),
		Port:           getOrDefault("PORT", "8080"),
		// Legacy single-provider
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
		LLMModel:     getOrDefault("LLM_MODEL", "gemini-1.5-flash"),
		// Multi-provider registry
		LLMProviderOrder: providerOrder,
		GroqAPIKey:       os.Getenv("GROQ_API_KEY"),
		DeepSeekAPIKey:   os.Getenv("DEEPSEEK_API_KEY"),
		MistralAPIKey:    os.Getenv("MISTRAL_API_KEY"),
		CerebrasAPIKey:   os.Getenv("CEREBRAS_API_KEY"),
		LLMSanitize:      os.Getenv("LLM_SANITIZE") != "false", // enabled by default
		LLMCustomRedact:  splitCSV(os.Getenv("LLM_CUSTOM_REDACT")),
		// Redis
		RedisAddr:     getOrDefault("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       redisDB,
	}, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func require(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("config: required environment variable %q is not set", key)
	}
	return v, nil
}

func getOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseGitlabProjectID(s string) int64 {
	if s == "" {
		return 0
	}
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}
