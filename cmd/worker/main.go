package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"pr-reviewer-ai/internal/app"
	"pr-reviewer-ai/internal/config"
)

func main() {
	// Force APP_ROLE to worker for this entry point
	os.Setenv("APP_ROLE", "worker")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	slog.Info("starting pr-reviewer-ai-worker")

	application, err := app.Build(cfg)
	if err != nil {
		log.Fatalf("startup error: %v", err)
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.RunWorker(ctx); err != nil {
		log.Fatalf("worker exited: %v", err)
	}
}
