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

	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/notifications"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
	workerpkg "github.com/jusso-dev/uptime/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	store := repository.NewPostgresStore(pool)
	m := metrics.New()
	registry := checks.NewRegistry(checks.Options{
		AllowPrivateTargets: cfg.AllowPrivateTargets,
		DefaultTimeout:      cfg.DefaultTimeout(),
		UserAgent:           cfg.HTTPUserAgent,
		TLSExpiryWarnDays:   cfg.TLSExpiryWarnDays,
	})
	notifier := notifications.NewService(store, cfg.AllowPrivateTargets)
	monitorSvc := service.NewMonitoringService(store, registry, notifier, m, true)
	w := workerpkg.New(cfg, store, monitorSvc, m, logger)

	metricsServer := &http.Server{Addr: ":8009", Handler: promhttp.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker metrics stopped", "error", err)
		}
	}()
	go func() {
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("worker stopped", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = metricsServer.Shutdown(shutdownCtx)
}
