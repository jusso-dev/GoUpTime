// Command uptime-migrate applies SQL migrations to the configured database.
// Migrations are tracked in a `schema_migrations` table so the same SQL file
// is never applied twice. Acquires a Postgres advisory lock for the
// duration so two replicas starting simultaneously can't race.
//
// Exit codes:
//
//	0 — all migrations up to date
//	1 — startup failure (config, db) or a migration failed mid-flight
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/repository"
)

// migrationsDir is overridable via env so a container can point at /app/migrations
// while local dev uses the repo-relative path.
const migrationsEnv = "MIGRATIONS_DIR"
const defaultMigrationsDir = "migrations"

// migrationLockID is an arbitrary 32-bit integer used for pg_advisory_lock.
// Same number must be used by every migrator instance — otherwise locks
// would not be exclusive between them.
const migrationLockID int64 = 9088008

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

	pool, err := repository.Open(ctx, cfg.DatabaseURL, repository.DefaultPoolConfig())
	if err != nil {
		logger.Error("database open failed", "error", err)
		return 1
	}
	defer pool.Close()

	dir := defaultMigrationsDir
	if env := os.Getenv(migrationsEnv); env != "" {
		dir = env
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		logger.Error("scan migrations directory", "dir", dir, "error", err)
		return 1
	}
	if len(files) == 0 {
		logger.Warn("no migrations found", "dir", dir)
		return 0
	}
	sort.Strings(files)

	if err := applyMigrations(ctx, pool, logger, files); err != nil {
		logger.Error("migration run failed", "error", err)
		return 1
	}
	logger.Info("migrations complete", "applied_count", len(files))
	return 0
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, files []string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return err
	}
	defer func() {
		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
			logger.Warn("release migration lock", "error", err)
		}
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return err
	}

	for _, file := range files {
		name := filepath.Base(file)
		var existing string
		err := conn.QueryRow(ctx, `SELECT filename FROM schema_migrations WHERE filename=$1`, name).Scan(&existing)
		if err == nil {
			logger.Info("migration already applied", "file", name)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			return err
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		logger.Info("migration applied", "file", name)
	}
	return nil
}
