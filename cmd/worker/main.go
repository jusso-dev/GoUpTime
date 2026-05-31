// Command uptime-worker runs the scheduler and check execution pool.
//
// Exit codes:
//
//	0 — clean shutdown after SIGINT/SIGTERM
//	1 — startup failure (config, db)
//	2 — runtime failure (worker or metrics server died unexpectedly)
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
	"github.com/jusso-dev/uptime/internal/service"
	workerpkg "github.com/jusso-dev/uptime/internal/worker"
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
		With("component", "worker", "env", cfg.AppEnv, "version", cfg.Version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := repository.Open(ctx, cfg.DatabaseURL, repository.DefaultPoolConfig())
	if err != nil {
		logger.Error("database open failed", "error", err)
		return 1
	}
	defer pool.Close()

	store := repository.NewPostgresStore(pool)
	m := metrics.New()

	// Redis is optional for the worker today (browser-check submission is
	// the only consumer). If the URL is unreachable the worker still runs
	// every non-browser check type — browser monitors will simply report
	// "queue unavailable" until Redis is back.
	var redisClient *redis.Client
	if opts, err := redis.ParseURL(cfg.RedisURL); err == nil {
		redisClient = redis.NewClient(opts)
		defer redisClient.Close()
	} else {
		logger.Warn("redis url invalid; browser checks disabled", "error", err)
	}

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
	q := queue.New(redisClient)
	dispatcher := notifications.NewDispatcher(store, logger,
		notifications.NewWebhookProvider(notifier),
		notifications.NewSlackProvider(cfg.HTTPUserAgent, cfg.WebhookTimeout()),
		notifications.NewPushProvider(store, cfg.HTTPUserAgent, cfg.ExpoAccessToken, cfg.WebhookTimeout()),
	)
	monitorSvc := service.NewMonitoringService(store, registry, notifier, m, true).
		WithRegion(cfg.WorkerRegion).
		WithQueue(q).
		WithDispatcher(dispatcher, cfg.AppBaseURL)
	w := workerpkg.New(cfg, store, monitorSvc, m, logger)

	metricsServer := &http.Server{
		Addr:              cfg.MetricsAddr(),
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	metricsErr := make(chan error, 1)
	workerErr := make(chan error, 1)

	go func() {
		logger.Info("metrics listening", "addr", cfg.MetricsAddr())
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			metricsErr <- err
		}
		close(metricsErr)
	}()
	go func() {
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			workerErr <- err
		}
		close(workerErr)
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
	case err, ok := <-workerErr:
		if ok && err != nil {
			logger.Error("worker crashed", "error", err)
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
	// Wait for worker to drain.
	if err, ok := <-workerErr; ok && err != nil {
		logger.Error("worker exited with error", "error", err)
	}
	logger.Info("worker stopped")
	return exitCode
}
