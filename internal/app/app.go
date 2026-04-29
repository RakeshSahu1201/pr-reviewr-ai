// Package app assembles the application by wiring all dependencies together.
// This keeps main.go as a two-liner and makes the wiring fully testable.
package app

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"pr-reviewer-ai/ent"
	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/config"
	appCrypto "pr-reviewer-ai/internal/crypto"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	pgRepo "pr-reviewer-ai/internal/repository/postgres"
	"pr-reviewer-ai/internal/server"
	"database/sql"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
)

// App holds the fully wired application and exposes a single Run method.
type App struct {
	cfg    *config.Config
	db     *ent.Client
	server *server.Server
}

// Build connects to Postgres (via ent) and wires all dependencies.
func Build(cfg *config.Config) (*App, error) {
	// --- Ent client (uses database/sql under the hood via pgx driver) ---
	sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("app: failed to open sql db: %w", err)
	}
	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	db := ent.NewClient(ent.Driver(drv))

	// Verify connectivity.
	if err := db.Schema.Create(context.Background()); err != nil {
		// We don't run auto-migrations; if schema tables are missing it'll fail here
		// with a clear error. Existing tables are left untouched.
		log.Printf("⚠ ent schema check: %v (continuing — managed by SQL migrations)", err)
	}

	// --- Repository layer ---
	tokenRepo := pgRepo.NewTokenRepo(db)
	logRepo := pgRepo.NewReviewLogRepo(db)

	// --- Crypto ---
	encryptFn := func(plain string) (string, error) { return appCrypto.Encrypt(cfg.EncryptionKey, plain) }
	decryptFn := func(enc string) (string, error) { return appCrypto.Decrypt(cfg.EncryptionKey, enc) }

	// --- Auth service ---
	authSvc := auth.NewAuthService(tokenRepo, encryptFn, decryptFn)

	// --- JWT service ---
	jwtSvc := server.NewJWTService(cfg.JWTSecret, cfg.JWTExpiryHours)

	// --- LLM pipeline ---
	pipeline := buildLLMPipeline(cfg)

	// --- GitProvider factory ---
	gitFactory := buildGitFactory(cfg)

	// --- HTTP server ---
	srv := server.New(authSvc, logRepo, jwtSvc, gitFactory, pipeline)

	return &App{cfg: cfg, db: db, server: srv}, nil
}

// Run starts the HTTP server and blocks until it exits.
func (a *App) Run() error {
	return http.ListenAndServe(":"+a.cfg.Port, a.server)
}

// Close releases the ent client / database connection pool.
func (a *App) Close() { a.db.Close() }

// buildLLMPipeline constructs the LLM pipeline from config.
func buildLLMPipeline(cfg *config.Config) *llm.Pipeline {
	keyMap := map[llm.ProviderName]string{
		llm.ProviderGemini:   cfg.GeminiAPIKey,
		llm.ProviderGroq:     cfg.GroqAPIKey,
		llm.ProviderDeepSeek: cfg.DeepSeekAPIKey,
		llm.ProviderMistral:  cfg.MistralAPIKey,
		llm.ProviderCerebras: cfg.CerebrasAPIKey,
	}

	var entries []llm.ProviderEntry
	for _, name := range cfg.LLMProviderOrder {
		pn := llm.ProviderName(name)
		key := keyMap[pn]
		if key == "" {
			continue
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
func buildGitFactory(cfg *config.Config) server.GitProviderFactory {
	return func(webUrl, token string) (git.GitProvider, error) {
		if cfg.GitLabProject == "" {
			return nil, fmt.Errorf("GITLAB_PROJECT_ID is not set")
		}
		// If webUrl is empty (should not happen with new auth), fallback to config.
		if webUrl == "" {
			webUrl = cfg.GitLabBaseURL
		}
		return git.NewGitLabProvider(webUrl, token, cfg.GitLabProject)
	}
}
