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

	"github.com/redis/go-redis/v9"

	"github.com/jusso-dev/uptime/internal/api"
	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/notifications"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
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
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr(cfg.RedisURL)})
	defer redisClient.Close()

	m := metrics.New()
	registry := checks.NewRegistry(checks.Options{
		AllowPrivateTargets: cfg.AllowPrivateTargets,
		DefaultTimeout:      cfg.DefaultTimeout(),
		UserAgent:           cfg.HTTPUserAgent,
		TLSExpiryWarnDays:   cfg.TLSExpiryWarnDays,
	})
	notifier := notifications.NewService(store, cfg.AllowPrivateTargets)
	monitorSvc := service.NewMonitoringService(store, registry, notifier, m, true)
	router := api.NewRouter(cfg, store, redisClient, monitorSvc, m, logger)

	server := &http.Server{Addr: cfg.Addr(), Handler: router, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("api listening", "addr", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api stopped", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func redisAddr(redisURL string) string {
	opt, err := redis.ParseURL(redisURL)
	if err != nil || opt.Addr == "" {
		return "localhost:6379"
	}
	return opt.Addr
}
