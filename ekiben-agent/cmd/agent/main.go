package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ekiben-agent/internal/agent"
	"ekiben-agent/internal/config"
	"ekiben-agent/internal/db"
)

func main() {
	cfg := config.FromFlags()

	logger := log.New(os.Stdout, "agent ", log.LstdFlags|log.Lmicroseconds)
	cfg.SourceMode = strings.TrimSpace(strings.ToLower(cfg.SourceMode))

	if cfg.SourceMode == "" {
		logger.Fatal("missing required --source (direct|api)")
	}

	var sqlDB *sql.DB
	var apiClient *db.APIClient
	var err error

	switch cfg.SourceMode {
	case "direct":
		sqlDB, err = db.Open(cfg.DBPath)
		if err != nil {
			logger.Fatalf("open db: %v", err)
		}
		defer sqlDB.Close()
	case "api":
		apiClient, err = db.NewAPIClient(cfg.APIBaseURL, cfg.APIToken)
		if err != nil {
			logger.Fatalf("configure tls api client: %v", err)
		}
	default:
		logger.Fatalf("invalid --source %q (expected direct or api)", cfg.SourceMode)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ag := agent.New(cfg, sqlDB, apiClient, logger)
	if err := ag.Run(ctx); err != nil {
		logger.Fatalf("agent exited: %v", err)
	}
}
