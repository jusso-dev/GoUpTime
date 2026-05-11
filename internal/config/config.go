package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                     string
	AppPort                    string
	Version                    string
	DatabaseURL                string
	RedisURL                   string
	BootstrapAPIKey            string
	AllowPrivateTargets        bool
	CheckWorkerCount           int
	DefaultCheckTimeoutSeconds int
	LogLevel                   slog.Level
	HTTPUserAgent              string
	TLSExpiryWarnDays          int
}

func Load() (Config, error) {
	level := slog.LevelInfo
	switch strings.ToLower(getenv("LOG_LEVEL", "info")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	cfg := Config{
		AppEnv:                     getenv("APP_ENV", "development"),
		AppPort:                    getenv("APP_PORT", "8008"),
		Version:                    getenv("APP_VERSION", "dev"),
		DatabaseURL:                getenv("DATABASE_URL", "postgres://uptime:uptime@localhost:5432/uptime?sslmode=disable"),
		RedisURL:                   getenv("REDIS_URL", "redis://localhost:6379/0"),
		BootstrapAPIKey:            getenv("UPTIME_BOOTSTRAP_API_KEY", "dev_admin_key"),
		AllowPrivateTargets:        getenvBool("ALLOW_PRIVATE_TARGETS", false),
		CheckWorkerCount:           getenvInt("CHECK_WORKER_COUNT", 10),
		DefaultCheckTimeoutSeconds: getenvInt("DEFAULT_CHECK_TIMEOUT_SECONDS", 10),
		LogLevel:                   level,
		HTTPUserAgent:              getenv("HTTP_USER_AGENT", "UpTime-Monitor/1.0"),
		TLSExpiryWarnDays:          getenvInt("TLS_EXPIRY_WARN_DAYS", 14),
	}
	if cfg.CheckWorkerCount < 1 {
		return Config{}, fmt.Errorf("CHECK_WORKER_COUNT must be greater than zero")
	}
	if cfg.DefaultCheckTimeoutSeconds < 1 {
		return Config{}, fmt.Errorf("DEFAULT_CHECK_TIMEOUT_SECONDS must be greater than zero")
	}
	return cfg, nil
}

func (c Config) Addr() string {
	return ":" + c.AppPort
}

func (c Config) DefaultTimeout() time.Duration {
	return time.Duration(c.DefaultCheckTimeoutSeconds) * time.Second
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
