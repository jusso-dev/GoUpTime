package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/repository"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	pool, err := repository.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	files, err := filepath.Glob("migrations/*.up.sql")
	if err != nil {
		panic(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		logger.Error("acquire database connection failed", "error", err)
		os.Exit(1)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(9088008)`); err != nil {
		logger.Error("migration lock failed", "error", err)
		os.Exit(1)
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock(9088008)`)

	for _, file := range files {
		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			panic(err)
		}
		if _, err := conn.Exec(ctx, string(sqlBytes)); err != nil && !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "duplicate key value") {
			logger.Error("migration failed", "file", file, "error", err)
			os.Exit(1)
		}
		logger.Info("migration applied", "file", file)
	}
	fmt.Println("migrations complete")
}
