package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ekiben-agent/internal/agent"
	"ekiben-agent/internal/config"
	"ekiben-agent/internal/db"
)

func main() {
	cfg := config.FromFlags()

	logger := log.New(os.Stdout, "agent ", log.LstdFlags|log.Lmicroseconds)

	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		logger.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ag := agent.New(cfg, sqlDB, logger)
	if err := ag.Run(ctx); err != nil {
		logger.Fatalf("agent exited: %v", err)
	}
}
