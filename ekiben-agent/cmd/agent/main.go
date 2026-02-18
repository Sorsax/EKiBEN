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
	console.SetTitle("EKiBEN Agent [" + version.Version + "]")
	var err error
	printLine := func(fn func(string, ...interface{}), delay time.Duration, format string, args ...interface{}) {
		fn(format, args...)
		time.Sleep(delay)
	}

	ok, err := console.EnsureSingleInstance("Global\\EKiBEN-Agent")
	if err != nil {
		log.Warnf("Single-instance check failed: %v", err)
	}

	// Determine environment from version
	environment := "Development"
	if strings.HasPrefix(version.Version, "SR") {
		environment = "Production"
	}

	// Print startup banner
	log.Headerf("===============================")
	log.Headerf("EKiBEN Agent version %s", log.Accent(version.Version))
	log.Headerf("Environment: %s", log.Accent(environment))
	log.Headerf("===============================")
	log.Infof("Agent starting up...")
	time.Sleep(250 * time.Millisecond)
	if err == nil && !ok {
		printLine(log.Warnf, 250*time.Millisecond, "Multiple instances detected, exiting...")
		time.Sleep(3 * time.Second)
		return
	}
	printLine(log.Infof, 250*time.Millisecond, "DB path: %s", log.Accent(cfg.DBPath))
	printLine(log.Infof, 250*time.Millisecond, "Press Ctrl+C to shut down")

	cfg.SourceMode = strings.TrimSpace(strings.ToLower(cfg.SourceMode))

	if cfg.SourceMode == "" {
		log.Fatalf("missing required --source (direct|api)")
	}

	var sqlDB *sql.DB
	var apiClient *db.APIClient
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
	shutdownStarted := make(chan struct{})
	shutdownDone := make(chan struct{})
	shutdown := func(reason console.ShutdownReason) {
		shutdownOnce.Do(func() {
			close(shutdownStarted)
			log.Infof("Gracefully shutting down...")
			time.Sleep(500 * time.Millisecond)
			log.Infof("Finishing in-flight writes...")
			ag.BeginShutdown()
			waitTimeout := 10 * time.Second
			if reason == console.ShutdownClose {
				waitTimeout = 2 * time.Second
			}
			if ok := ag.WaitForInflight(waitTimeout); !ok {
				log.Warnf("Timed out waiting for in-flight work")
			}
			time.Sleep(500 * time.Millisecond)
			log.Infof("Closing websocket connections...")
			time.Sleep(500 * time.Millisecond)
			log.Infof("Exiting...")
			exitDelay := 3 * time.Second
			if reason == console.ShutdownClose {
				exitDelay = 1 * time.Second
			}
			time.Sleep(exitDelay)
			close(shutdownDone)
			os.Exit(0)
		})
	}

	console.RegisterShutdown(shutdown)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		shutdown(console.ShutdownCtrlC)
	}()

	if err := ag.Run(ctx); err != nil {
		log.Fatalf("agent exited: %v", err)
	}

	select {
	case <-shutdownStarted:
		<-shutdownDone
	default:
	}
}
