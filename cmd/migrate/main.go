// Command uptime-migrate applies ORM-managed schema migrations.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/repository"
)

func main() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

func run() int {
	bootLogger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		bootLogger.Error("config load failed", "error", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With("component", "migrate")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db, err := repository.Open(ctx, cfg.DatabaseURL, repository.DefaultPoolConfig())
	if err != nil {
		logger.Error("database open failed", "error", err)
		return 1
	}
	defer func() {
		if err := repository.Close(db); err != nil {
			logger.Warn("database close failed", "error", err)
		}
	}()

	if err := repository.AutoMigrate(ctx, db); err != nil {
		logger.Error("schema migration failed", "error", err)
		return 1
	}
	logger.Info("schema migration complete")
	return 0
}
