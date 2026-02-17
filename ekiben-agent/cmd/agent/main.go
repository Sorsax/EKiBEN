package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ekiben-agent/internal/agent"
	"ekiben-agent/internal/console"
	"ekiben-agent/internal/config"
	"ekiben-agent/internal/db"
	"ekiben-agent/internal/logger"
	"ekiben-agent/internal/version"
)

func main() {
	cfg := config.FromFlags()
	useColor := console.EnableANSI()
	log := logger.New(os.Stdout, cfg.LogTraffic, useColor)
	console.SetTitle("EKiBEN Agent")

	// Determine environment from version
	environment := "Development"
	if strings.HasPrefix(version.Version, "SR") {
		environment = "Production"
	}

	// Print startup banner
	log.Headerf("EKiBEN Agent version %s", log.Accent(version.Version))
	log.Headerf("Environment: %s", log.Accent(environment))
	log.Infof("Agent starting up...")
	log.Infof("DB path: %s", log.Accent(cfg.DBPath))
	log.Infof("Press Ctrl+C to shut down")

	cfg.SourceMode = strings.TrimSpace(strings.ToLower(cfg.SourceMode))

	if cfg.SourceMode == "" {
		log.Fatalf("missing required --source (direct|api)")
	}

	var sqlDB *sql.DB
	var apiClient *db.APIClient
	var err error

	switch cfg.SourceMode {
	case "direct":
		sqlDB, err = db.Open(cfg.DBPath)
		if err != nil {
			log.Fatalf("open db: %v", err)
		}
		defer sqlDB.Close()
	case "api":
		apiClient, err = db.NewAPIClient(cfg.APIBaseURL, cfg.APIToken)
		if err != nil {
			log.Fatalf("configure tls api client: %v", err)
		}
	default:
		log.Fatalf("invalid --source %q (expected direct or api)", cfg.SourceMode)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ag := agent.New(cfg, sqlDB, apiClient, log)
	if err := ag.Run(ctx); err != nil {
		log.Fatalf("agent exited: %v", err)
	}
}
