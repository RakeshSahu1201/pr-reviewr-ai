package main

import (
	"log"

	"pr-reviewer-ai/internal/app"
	"pr-reviewer-ai/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	application, err := app.Build(cfg)
	if err != nil {
		log.Fatalf("startup error: %v", err)
	}
	defer application.Close()

	log.Printf("🚀 pr-reviewer-ai listening on :%s", cfg.Port)
	if err := application.Run(); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
