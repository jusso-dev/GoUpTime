// Command uptime-scheduler runs the leader-elected dispatcher that
// pushes due monitor checks onto per-region Redis queues. It also
// hosts the cross-region aggregator that listens to results and
// confirms failures across the configured regions.
//
// Run one replica per process; only one wins the lock. The rest stand
// by and take over within leaderTTL (~10s) if the leader dies.
//
// Exit codes:
//
//	0 — clean shutdown
//	1 — startup failure (config, db, redis)
//	2 — runtime failure (scheduler or aggregator died unexpectedly)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/notifications"
	"github.com/jusso-dev/uptime/internal/queue"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/scheduler"
	"github.com/jusso-dev/uptime/internal/service"
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
		With("component", "scheduler", "env", cfg.AppEnv, "version", cfg.Version)

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
	q := queue.New(redisClient)

	m := metrics.New()
	// The aggregator needs a monitoring service so it can call back into
	// the existing incident-rule path. Notifier shares the API's webhook
	// dispatch since the scheduler is the natural single source of truth
	// for cross-region incident state.
	registry := checks.NewRegistry(checks.Options{
		AllowPrivateTargets: cfg.AllowPrivateTargets,
		DefaultTimeout:      cfg.DefaultTimeout(),
		UserAgent:           cfg.HTTPUserAgent,
		TLSExpiryWarnDays:   cfg.TLSExpiryWarnDays,
		HeartbeatStore:      store,
		MultistepStore:      store,
		BrowserStore:        store,
		Redis:               redisClient,
		BrowserEnabled:      cfg.BrowserCheckEnabled,
		ICMPEnabled:         cfg.ICMPCheckEnabled,
	})
	notifier := notifications.NewService(store, notifications.Options{
		AllowPrivateTargets: cfg.AllowPrivateTargets,
		SigningSecret:       cfg.WebhookSigningSecret,
		PerAttemptTimeout:   cfg.WebhookTimeout(),
		MaxRetries:          cfg.WebhookMaxRetries,
		UserAgent:           cfg.HTTPUserAgent,
	})
	monitorSvc := service.NewMonitoringService(store, registry, notifier, m, true)

	sched := scheduler.New(cfg, store, q, logger)
	agg := scheduler.NewAggregator(store, q, monitorSvc, logger)

	metricsServer := &http.Server{
		Addr:              cfg.MetricsAddr(),
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	metricsErr := make(chan error, 1)
	go func() {
		logger.Info("metrics listening", "addr", cfg.MetricsAddr())
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			metricsErr <- err
		}
		close(metricsErr)
	}()

	schedErr := make(chan error, 1)
	aggErr := make(chan error, 1)
	go func() {
		if err := sched.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			schedErr <- err
		}
		close(schedErr)
	}()
	go func() {
		if err := agg.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			aggErr <- err
		}
		close(aggErr)
	}()

	exitCode := 0
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err, ok := <-metricsErr:
		if ok && err != nil {
			logger.Error("metrics server crashed", "error", err)
			exitCode = 2
			stop()
		}
	case err, ok := <-schedErr:
		if ok && err != nil {
			logger.Error("scheduler crashed", "error", err)
			exitCode = 2
			stop()
		}
	case err, ok := <-aggErr:
		if ok && err != nil {
			logger.Error("aggregator crashed", "error", err)
			exitCode = 2
			stop()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer cancel()
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics shutdown failed", "error", err)
		_ = metricsServer.Close()
	}
	logger.Info("scheduler stopped")
	return exitCode
}
