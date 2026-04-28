// Package app assembles the application by wiring all dependencies together.
// This keeps main.go as a two-liner and makes the wiring fully testable.
package app

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/config"
	appCrypto "pr-reviewer-ai/internal/crypto"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	pgRepo "pr-reviewer-ai/internal/repository/postgres"
	"pr-reviewer-ai/internal/server"

	"github.com/jackc/pgx/v5/pgxpool"
)

// App holds the fully wired application and exposes a single Run method.
type App struct {
	cfg    *config.Config
	pool   *pgxpool.Pool
	server *server.Server
}

// Build connects to Postgres and wires all dependencies.
func Build(cfg *config.Config) (*App, error) {
	// --- Postgres connection pool ---
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("app: failed to create DB pool: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("app: database ping failed: %w", err)
	}

	// --- Repository layer ---
	tokenRepo := pgRepo.NewTokenRepo(pool)
	logRepo := pgRepo.NewReviewLogRepo(pool)

	// --- Crypto ---
	encryptFn := func(plain string) (string, error) { return appCrypto.Encrypt(cfg.EncryptionKey, plain) }
	decryptFn := func(enc string) (string, error) { return appCrypto.Decrypt(cfg.EncryptionKey, enc) }

	// --- Auth service ---
	authSvc := auth.NewAuthService(tokenRepo, encryptFn, decryptFn)

	// --- JWT service ---
	jwtSvc := server.NewJWTService(cfg.JWTSecret, cfg.JWTExpiryHours)

	// --- LLM pipeline ---
	// Build an ordered failover registry from the configured provider list.
	// Wrapped in SanitizationMiddleware when LLMSanitize == true.
	pipeline := buildLLMPipeline(cfg)

	// --- GitProvider factory ---
	gitFactory := buildGitFactory(cfg)

	// --- HTTP server ---
	srv := server.New(authSvc, logRepo, jwtSvc, gitFactory, pipeline)

	return &App{cfg: cfg, pool: pool, server: srv}, nil
}

// Run starts the HTTP server and blocks until it exits.
func (a *App) Run() error {
	return http.ListenAndServe(":"+a.cfg.Port, a.server)
}

// Close releases the database connection pool.
func (a *App) Close() { a.pool.Close() }

// buildLLMPipeline constructs the LLM pipeline from config.
// It builds an ordered Registry with automatic failover and optional sanitization.
// Returns nil when no provider API keys are configured (pipeline disabled → stub).
func buildLLMPipeline(cfg *config.Config) *llm.Pipeline {
	// Map provider names to their API keys.
	keyMap := map[llm.ProviderName]string{
		llm.ProviderGemini:   cfg.GeminiAPIKey,
		llm.ProviderGroq:     cfg.GroqAPIKey,
		llm.ProviderDeepSeek: cfg.DeepSeekAPIKey,
		llm.ProviderMistral:  cfg.MistralAPIKey,
		llm.ProviderCerebras: cfg.CerebrasAPIKey,
	}

	// Build ordered ProviderEntry slice from config order.
	var entries []llm.ProviderEntry
	for _, name := range cfg.LLMProviderOrder {
		pn := llm.ProviderName(name)
		key := keyMap[pn]
		if key == "" {
			continue // skip providers with no key
		}
		entries = append(entries, llm.ProviderEntry{Name: pn, APIKey: key})
	}

	if len(entries) == 0 {
		log.Println("⚠ No LLM API keys set — pipeline disabled, using diff-stats stub")
		return nil
	}

	client, err := llm.BuildRegistry(llm.RegistryConfig{
		Providers:          entries,
		EnableSanitization: cfg.LLMSanitize,
		CustomRedactTerms:  cfg.LLMCustomRedact,
	})
	if err != nil {
		log.Printf("⚠ LLM registry build failed: %v — pipeline disabled", err)
		return nil
	}

	log.Printf("✓ LLM registry active — providers: %v (sanitize=%v)", cfg.LLMProviderOrder, cfg.LLMSanitize)
	return llm.NewPipeline(client)
}

// buildGitFactory returns a per-request GitProvider factory.
// Swap internals here to support GitHub / MCP without touching business logic.
func buildGitFactory(cfg *config.Config) server.GitProviderFactory {
	return func(token string) (git.GitProvider, error) {
		if cfg.GitLabProject == "" {
			return nil, fmt.Errorf("GITLAB_PROJECT_ID is not set")
		}
		return git.NewGitLabProvider(cfg.GitLabBaseURL, token, cfg.GitLabProject)
	}
}
