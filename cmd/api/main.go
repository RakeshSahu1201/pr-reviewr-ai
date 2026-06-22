package main

import (
	"log"
	"log/slog"
	"os"

	"pr-reviewer-ai/internal/app"
	"pr-reviewer-ai/internal/config"
)

func main() {
	// Force APP_ROLE to api for this entry point
	os.Setenv("APP_ROLE", "api")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	slog.Info("starting pr-reviewer-ai-api", "port", cfg.Port)

	application, err := app.Build(cfg)
	if err != nil {
		log.Fatalf("startup error: %v", err)
	}
	defer application.Close()

	if err := application.RunAPI(); err != nil {
		log.Fatalf("api server exited: %v", err)
	}
}
