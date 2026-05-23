// Command uptime-api runs the public HTTP API.
//
// Exit codes:
//
//	0 — clean shutdown after SIGINT/SIGTERM
//	1 — startup failure (config, db, redis)
//	2 — runtime failure (listener died unexpectedly)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jusso-dev/uptime/internal/api"
	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/notifications"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
)

func main() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

func run() int {
	// Bootstrap logger; replaced once config tells us the desired level.
	bootLogger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		bootLogger.Error("config load failed", "error", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With("component", "api", "env", cfg.AppEnv, "version", cfg.Version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := repository.Open(ctx, cfg.DatabaseURL, repository.DefaultPoolConfig())
	if err != nil {
		logger.Error("database open failed", "error", err)
		return 1
	}
	defer pool.Close()
	store := repository.NewPostgresStore(pool)

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Error("redis url invalid", "error", err)
		return 1
	}
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		// Redis is optional for the API surface; log a warning and continue
		// so a transient outage doesn't take the API down.
		logger.Warn("redis ping failed; continuing without redis health", "error", err)
	}
	pingCancel()

	m := metrics.New()
	registry := checks.NewRegistry(checks.Options{
		AllowPrivateTargets: cfg.AllowPrivateTargets,
		DefaultTimeout:      cfg.DefaultTimeout(),
		UserAgent:           cfg.HTTPUserAgent,
		TLSExpiryWarnDays:   cfg.TLSExpiryWarnDays,
	})
	notifier := notifications.NewService(store, notifications.Options{
		AllowPrivateTargets: cfg.AllowPrivateTargets,
		SigningSecret:       cfg.WebhookSigningSecret,
		PerAttemptTimeout:   cfg.WebhookTimeout(),
		MaxRetries:          cfg.WebhookMaxRetries,
		UserAgent:           cfg.HTTPUserAgent,
	})
	monitorSvc := service.NewMonitoringService(store, registry, notifier, m, true)

	var clerkVerifier *auth.ClerkVerifier
	if cfg.ClerkEnabled {
		// Fetching JWKS on startup is best-effort: if the network is flaky
		// the keyfunc loader retries lazily. We log but don't fail boot so
		// a brief Clerk outage doesn't take the API down.
		v, err := auth.NewClerkVerifier(ctx, cfg.ClerkIssuer)
		if err != nil {
			logger.Error("clerk verifier init failed; continuing without clerk auth", "error", err)
		} else {
			clerkVerifier = v
			logger.Info("clerk auth enabled", "issuer", cfg.ClerkIssuer)
		}
	}

	router := api.NewRouter(cfg, store, redisClient, monitorSvc, m, logger, clerkVerifier)

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           router,
		ReadHeaderTimeout: time.Duration(cfg.APIReadHeaderTimeoutSec) * time.Second,
		WriteTimeout:      time.Duration(cfg.APIWriteTimeoutSec) * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err, ok := <-serverErr:
		if ok && err != nil {
			logger.Error("api server crashed", "error", err)
			return 2
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		// Force-close listeners; don't return 1 — we still received a
		// shutdown signal.
		_ = server.Close()
	}
	logger.Info("api stopped")
	// Drain any pending error so we don't leak the goroutine.
	if err, ok := <-serverErr; ok && err != nil {
		fmt.Fprintln(os.Stderr, "post-shutdown error:", err)
	}
	return 0
}
