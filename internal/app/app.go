// Package app assembles the application by wiring all dependencies together.
// This keeps main.go as a two-liner and makes the wiring fully testable.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/redis/go-redis/v9"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql

	"pr-reviewer-ai/ent"
	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/cache"
	"pr-reviewer-ai/internal/config"
	appCrypto "pr-reviewer-ai/internal/crypto"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	pgRepo "pr-reviewer-ai/internal/repository/postgres"
	"pr-reviewer-ai/internal/ratelimit"
	"pr-reviewer-ai/internal/server"
)

// App holds the fully wired application and exposes a single Run method.
type App struct {
	cfg    *config.Config
	db     *ent.Client
	rdb    *redis.Client
	server *server.Server
}

// Build connects to Postgres (via ent) and Redis, then wires all dependencies.
func Build(cfg *config.Config) (*App, error) {
	// ─── Ent / Postgres client ────────────────────────────────────────────────
	sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("app: failed to open sql db: %w", err)
	}
	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	db := ent.NewClient(ent.Driver(drv))

	if err := db.Schema.Create(context.Background()); err != nil {
		log.Printf("⚠ ent schema check: %v (continuing — managed by SQL migrations)", err)
	}

	// ─── Redis client ─────────────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// Verify connectivity — non-fatal: the app can still serve requests from Postgres.
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("⚠ Redis unreachable at %s: %v — caching and rate limiting degraded", cfg.RedisAddr, err)
	} else {
		log.Printf("✓ Redis connected at %s (db %d)", cfg.RedisAddr, cfg.RedisDB)
	}

	// ─── Repository layer ─────────────────────────────────────────────────────
	tokenRepo := pgRepo.NewTokenRepo(db)
	logRepo := pgRepo.NewReviewLogRepo(db)

	// ─── Crypto helpers ───────────────────────────────────────────────────────
	encryptFn := func(plain string) (string, error) { return appCrypto.Encrypt(cfg.EncryptionKey, plain) }
	decryptFn := func(enc string) (string, error) { return appCrypto.Decrypt(cfg.EncryptionKey, enc) }

	// ─── Auth service ─────────────────────────────────────────────────────────
	authSvc := auth.NewAuthService(tokenRepo, encryptFn, decryptFn)

	// ─── JWT service ──────────────────────────────────────────────────────────
	jwtSvc := server.NewJWTService(cfg.JWTSecret, cfg.JWTExpiryHours)

	// ─── Redis-backed session store ───────────────────────────────────────────
	sessionStore := cache.NewSessionStore(rdb, encryptFn, decryptFn)

	// ─── Sliding-window rate limiter ──────────────────────────────────────────
	limiter := ratelimit.New(rdb)

	// ─── LLM pipeline ─────────────────────────────────────────────────────────
	pipeline := buildLLMPipeline(cfg)

	// ─── GitProvider factory ──────────────────────────────────────────────────
	gitFactory := buildGitFactory(cfg)

	// ─── HTTP server ──────────────────────────────────────────────────────────
	srv := server.New(authSvc, logRepo, jwtSvc, gitFactory, pipeline, sessionStore, limiter)

	return &App{cfg: cfg, db: db, rdb: rdb, server: srv}, nil
}

// Run starts the HTTP server and blocks until it exits.
func (a *App) Run() error {
	return http.ListenAndServe(":"+a.cfg.Port, a.server)
}

// Close releases the ent client / database connection pool and the Redis client.
func (a *App) Close() {
	a.db.Close()
	if a.rdb != nil {
		_ = a.rdb.Close()
	}
}

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
	return func(webUrl, token string, projectID int64) (git.GitProvider, error) {
		if projectID <= 0 {
			projectID = cfg.GitLabProject
		}
		if webUrl == "" {
			webUrl = cfg.GitLabBaseURL
		}
		return git.NewGitLabProvider(webUrl, token, projectID)
	}
}
