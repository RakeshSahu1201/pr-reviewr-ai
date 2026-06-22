// Package app assembles the application by wiring all dependencies together.
// This keeps main.go as a two-liner and makes the wiring fully testable.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
	"github.com/redis/go-redis/v9"

	"pr-reviewer-ai/ent"
	"pr-reviewer-ai/internal/agent"
	"pr-reviewer-ai/internal/auth"
	"pr-reviewer-ai/internal/cache"
	"pr-reviewer-ai/internal/config"
	appCrypto "pr-reviewer-ai/internal/crypto"
	"pr-reviewer-ai/internal/git"
	"pr-reviewer-ai/internal/llm"
	"pr-reviewer-ai/internal/ratelimit"
	pgRepo "pr-reviewer-ai/internal/repository/postgres"
	"pr-reviewer-ai/internal/server"
)

// App holds the fully wired application and exposes granular Run methods.
type App struct {
	cfg    *config.Config
	db     *ent.Client
	rdb    *redis.Client
	server *server.Server
	worker *agent.Worker
	log    *slog.Logger
}

// Build connects to Postgres (via ent) and Redis, then wires all dependencies.
// Only the subsystems required by cfg.AppRole are constructed; the rest are left nil.
func Build(cfg *config.Config) (*App, error) {
	// ─── Structured logger — pre-stamp every entry with the active role ────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).
		With("app_role", cfg.AppRole)

	// ─── Ent / Postgres client ────────────────────────────────────────────────
	sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("app: failed to open sql db: %w", err)
	}
	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	db := ent.NewClient(ent.Driver(drv))

	if err := db.Schema.Create(context.Background()); err != nil {
		logger.Warn("ent schema check failed (continuing — managed by SQL migrations)", "err", err)
	}

	// ─── Redis client ─────────────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// Verify connectivity — non-fatal: the app can still serve requests from Postgres.
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logger.Warn("Redis unreachable — caching and rate limiting degraded",
			"addr", cfg.RedisAddr, "err", err)
	} else {
		logger.Info("Redis connected", "addr", cfg.RedisAddr, "db", cfg.RedisDB)
	}

	// ─── Repository layer ─────────────────────────────────────────────────────
	tokenRepo := pgRepo.NewTokenRepo(db)
	logRepo := pgRepo.NewReviewLogRepo(db)

	// ─── Crypto helpers ───────────────────────────────────────────────────────
	encryptFn := func(plain string) (string, error) { return appCrypto.Encrypt(cfg.EncryptionKey, plain) }
	decryptFn := func(enc string) (string, error) { return appCrypto.Decrypt(cfg.EncryptionKey, enc) }

	// ─── Auth service ─────────────────────────────────────────────────────────
	authSvc := auth.NewAuthService(tokenRepo, encryptFn, decryptFn)

	// ─── GitProvider factory ──────────────────────────────────────────────────
	gitFactory := buildGitFactory(cfg)

	// ─── LLM pipeline ─────────────────────────────────────────────────────────
	pipeline := buildLLMPipeline(cfg, logger)

	app := &App{cfg: cfg, db: db, rdb: rdb, log: logger}

	// ─── Role-gated: Worker (worker | standalone only) ────────────────────────
	if cfg.AppRole == "worker" || cfg.AppRole == "standalone" {
		app.worker = agent.NewWorker(authSvc, logRepo, gitFactory, pipeline, logger)
	}

	// ─── Role-gated: HTTP Server (api | standalone only) ─────────────────────
	if cfg.AppRole == "api" || cfg.AppRole == "standalone" {
		jwtSvc := server.NewJWTService(cfg.JWTSecret, cfg.JWTExpiryHours)
		sessionStore := cache.NewSessionStore(rdb, encryptFn, decryptFn)
		limiter := ratelimit.New(rdb)
		app.server = server.New(authSvc, logRepo, jwtSvc, gitFactory, pipeline, sessionStore, limiter)
	}

	return app, nil
}

// Run starts the subsystems dictated by cfg.AppRole and blocks until they exit.
// This is kept for backward compatibility and "standalone" mode.
func (a *App) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch a.cfg.AppRole {
	case "api":
		return a.RunAPI()

	case "worker":
		return a.RunWorker(ctx)

	case "standalone":
		a.log.Info("booting in Standalone mode (API + Worker)", "port", a.cfg.Port)
		go a.RunWorker(ctx)
		return a.RunAPI()

	default:
		return fmt.Errorf("app: unknown APP_ROLE %q — must be one of: api | worker | standalone", a.cfg.AppRole)
	}
}

// RunAPI starts the HTTP server and blocks.
func (a *App) RunAPI() error {
	if a.server == nil {
		return fmt.Errorf("app: server not initialized (check APP_ROLE)")
	}
	a.log.Info("booting API server", "port", a.cfg.Port)
	return http.ListenAndServe(":"+a.cfg.Port, a.server)
}

// RunWorker starts the background polling loop and a minimal health check server.
func (a *App) RunWorker(ctx context.Context) error {
	if a.worker == nil {
		return fmt.Errorf("app: worker not initialized (check APP_ROLE)")
	}
	a.log.Info("booting background worker", "port", a.cfg.Port)

	// Start worker in its own goroutine
	go a.worker.Start(ctx, 10*time.Second)

	// Start a minimal health check server so the worker "listens" and satisfies docker healthchecks.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","role":"worker"}`))
	})

	server := &http.Server{
		Addr:    ":" + a.cfg.Port,
		Handler: mux,
	}

	// Handle graceful shutdown for the health server
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	return server.ListenAndServe()
}

// Close releases the ent client / database connection pool and the Redis client.
func (a *App) Close() {
	a.db.Close()
	if a.rdb != nil {
		_ = a.rdb.Close()
	}
}

// buildLLMPipeline constructs the LLM pipeline from config.
func buildLLMPipeline(cfg *config.Config, logger *slog.Logger) *llm.Pipeline {
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
		
		modelStr := ""
		if pn == llm.ProviderGemini {
			modelStr = cfg.LLMModel
		}
		
		entries = append(entries, llm.ProviderEntry{Name: pn, APIKey: key, Model: modelStr})
	}

	if len(entries) == 0 {
		logger.Warn("no LLM API keys set — pipeline disabled, using diff-stats stub")
		return nil
	}

	client, err := llm.BuildRegistry(llm.RegistryConfig{
		Providers:          entries,
		EnableSanitization: cfg.LLMSanitize,
		CustomRedactTerms:  cfg.LLMCustomRedact,
	})
	if err != nil {
		logger.Error("LLM registry build failed — pipeline disabled", "err", err)
		return nil
	}

	logger.Info("LLM registry active",
		"providers", cfg.LLMProviderOrder,
		"sanitize", cfg.LLMSanitize,
	)
	return llm.NewPipeline(client)
}

// buildGitFactory returns a per-request GitProvider factory.
func buildGitFactory(cfg *config.Config) server.GitProviderFactory {
	return func(webUrl, token string, projectID int64, gitlabUserID int) (git.GitProvider, error) {
		if projectID <= 0 {
			projectID = cfg.GitLabProject
		}
		if webUrl == "" {
			webUrl = cfg.GitLabBaseURL
		}
		return git.NewGitLabProvider(webUrl, token, projectID, gitlabUserID)
	}
}
