package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ag := agent.New(cfg, sqlDB, apiClient, log)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			log.Infof("Shutting down...")
			time.Sleep(500 * time.Millisecond)
			log.Infof("Finishing in-flight writes...")
			ag.BeginShutdown()
			if ok := ag.WaitForInflight(10 * time.Second); !ok {
				log.Warnf("Timed out waiting for in-flight work")
			}
			time.Sleep(500 * time.Millisecond)
			log.Infof("Closing websocket connections...")
			time.Sleep(500 * time.Millisecond)
			log.Infof("Exiting...")
			time.Sleep(3 * time.Second)
			os.Exit(0)
		})
	}

	console.RegisterShutdown(shutdown)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		shutdown()
	}()

	if err := ag.Run(ctx); err != nil {
		log.Fatalf("agent exited: %v", err)
	}
}
