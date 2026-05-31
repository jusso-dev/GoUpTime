package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration. Values are loaded from environment
// variables exactly once at startup via Load; the resulting Config should be
// treated as immutable.
type Config struct {
	AppEnv                     string
	AppPort                    string
	MetricsPort                string
	Version                    string
	DatabaseURL                string
	RedisURL                   string
	BootstrapAPIKey            string
	BootstrapOrgID             string
	AllowPrivateTargets        bool
	CheckWorkerCount           int
	DefaultCheckTimeoutSeconds int
	LogLevel                   slog.Level
	HTTPUserAgent              string
	TLSExpiryWarnDays          int
	WebhookSigningSecret       string
	WebhookTimeoutSeconds      int
	WebhookMaxRetries          int
	ShutdownTimeoutSeconds     int
	APIReadHeaderTimeoutSec    int
	APIWriteTimeoutSec         int
	MaxRequestBodyBytes        int64
	SchedulerTickSeconds       int

	// Clerk authentication. When ClerkEnabled is true the API verifies Clerk
	// session JWTs in addition to the existing API-key auth path. The
	// webhook secret is required to authenticate Clerk → app webhooks that
	// keep the local users/organizations mirror in sync.
	ClerkEnabled          bool
	ClerkIssuer           string
	ClerkSecretKey        string
	ClerkPublishableKey   string
	ClerkWebhookSecret    string

	// CORSAllowedOrigins is the explicit allow-list for the mobile/web
	// clients. Development mode (APP_ENV != production) additionally allows
	// localhost and Expo (exp://) origins for convenience.
	CORSAllowedOrigins []string

	// Check-type gates. Browser checks need the Playwright sidecar to be
	// running; ICMP needs ping_group_range/CAP_NET_RAW on the host. Both
	// default off so a fresh install doesn't emit spurious failures.
	BrowserCheckEnabled bool
	ICMPCheckEnabled    bool

	// AppBaseURL is the public-facing URL of the API. Used to build
	// heartbeat ping URLs and incident deep links surfaced to integrations
	// (Slack, email).
	AppBaseURL string

	// WorkerRegion is the label this worker process attaches to results,
	// heartbeats, and the Redis queue it consumes from. Defaults to
	// "default" for single-region installs.
	WorkerRegion string

	// ExpoAccessToken is an optional bearer token from
	// https://expo.dev/accounts/[org]/settings/access-tokens that raises
	// the per-second push rate limit. Empty is fine for low-volume sends.
	ExpoAccessToken string
}

// IsProduction returns true when APP_ENV indicates a non-development
// deployment. Used to enforce stricter defaults (no bootstrap key fallback,
// no plaintext credentials, etc.).
func (c Config) IsProduction() bool {
	switch strings.ToLower(c.AppEnv) {
	case "production", "prod":
		return true
	}
	return false
}

// Load reads configuration from the environment and returns a fully validated
// Config. Errors describe the offending variable and the constraint that
// failed so operators can fix misconfiguration without reading source.
func Load() (Config, error) {
	cfg := Config{
		AppEnv:                     getenv("APP_ENV", "development"),
		AppPort:                    getenv("APP_PORT", "8008"),
		MetricsPort:                getenv("METRICS_PORT", "8009"),
		Version:                    getenv("APP_VERSION", "dev"),
		DatabaseURL:                getenv("DATABASE_URL", "postgres://uptime:uptime@localhost:5432/uptime?sslmode=disable"),
		RedisURL:                   getenv("REDIS_URL", "redis://localhost:6379/0"),
		BootstrapAPIKey:            getenv("UPTIME_BOOTSTRAP_API_KEY", ""),
		BootstrapOrgID:             getenv("BOOTSTRAP_ORG_ID", "00000000-0000-0000-0000-000000000001"),
		HTTPUserAgent:              getenv("HTTP_USER_AGENT", "UpTime-Monitor/1.0"),
		WebhookSigningSecret:       getenv("WEBHOOK_SIGNING_SECRET", ""),
		ClerkIssuer:                strings.TrimRight(getenv("CLERK_ISSUER", ""), "/"),
		ClerkSecretKey:             getenv("CLERK_SECRET_KEY", ""),
		ClerkPublishableKey:        getenv("CLERK_PUBLISHABLE_KEY", ""),
		ClerkWebhookSecret:         getenv("CLERK_WEBHOOK_SECRET", ""),
		AppBaseURL:                 strings.TrimRight(getenv("APP_BASE_URL", "http://localhost:8008"), "/"),
		WorkerRegion:               getenv("WORKER_REGION", "default"),
		ExpoAccessToken:            getenv("EXPO_ACCESS_TOKEN", ""),
	}

	cfg.CORSAllowedOrigins = splitCSV(getenv("CORS_ALLOWED_ORIGINS", ""))

	level, err := parseLogLevel(getenv("LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	cfg.AllowPrivateTargets, err = getenvBool("ALLOW_PRIVATE_TARGETS", false)
	if err != nil {
		return Config{}, err
	}

	cfg.ClerkEnabled, err = getenvBool("CLERK_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.BrowserCheckEnabled, err = getenvBool("BROWSER_CHECK_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.ICMPCheckEnabled, err = getenvBool("ICMP_ENABLED", false)
	if err != nil {
		return Config{}, err
	}

	intVars := []struct {
		key      string
		fallback int
		min      int
		max      int
		dst      *int
	}{
		{"CHECK_WORKER_COUNT", 10, 1, 1024, &cfg.CheckWorkerCount},
		{"DEFAULT_CHECK_TIMEOUT_SECONDS", 10, 1, 300, &cfg.DefaultCheckTimeoutSeconds},
		{"TLS_EXPIRY_WARN_DAYS", 14, 1, 365, &cfg.TLSExpiryWarnDays},
		{"WEBHOOK_TIMEOUT_SECONDS", 10, 1, 60, &cfg.WebhookTimeoutSeconds},
		{"WEBHOOK_MAX_RETRIES", 3, 0, 10, &cfg.WebhookMaxRetries},
		{"SHUTDOWN_TIMEOUT_SECONDS", 15, 1, 120, &cfg.ShutdownTimeoutSeconds},
		{"API_READ_HEADER_TIMEOUT_SECONDS", 5, 1, 60, &cfg.APIReadHeaderTimeoutSec},
		{"API_WRITE_TIMEOUT_SECONDS", 30, 1, 600, &cfg.APIWriteTimeoutSec},
		{"SCHEDULER_TICK_SECONDS", 5, 1, 60, &cfg.SchedulerTickSeconds},
	}
	for _, v := range intVars {
		value, err := getenvInt(v.key, v.fallback)
		if err != nil {
			return Config{}, err
		}
		if value < v.min || value > v.max {
			return Config{}, fmt.Errorf("%s=%d is out of range [%d, %d]", v.key, value, v.min, v.max)
		}
		*v.dst = value
	}

	bodyBytes, err := getenvInt64("MAX_REQUEST_BODY_BYTES", 1<<20) // 1 MiB
	if err != nil {
		return Config{}, err
	}
	if bodyBytes < 1024 {
		return Config{}, fmt.Errorf("MAX_REQUEST_BODY_BYTES=%d must be >= 1024", bodyBytes)
	}
	cfg.MaxRequestBodyBytes = bodyBytes

	if err := validatePort(cfg.AppPort, "APP_PORT"); err != nil {
		return Config{}, err
	}
	if err := validatePort(cfg.MetricsPort, "METRICS_PORT"); err != nil {
		return Config{}, err
	}
	if cfg.AppPort == cfg.MetricsPort {
		return Config{}, fmt.Errorf("APP_PORT and METRICS_PORT must differ (both are %q)", cfg.AppPort)
	}
	if err := validateDatabaseURL(cfg.DatabaseURL); err != nil {
		return Config{}, err
	}
	if err := validateRedisURL(cfg.RedisURL); err != nil {
		return Config{}, err
	}

	if cfg.BootstrapAPIKey == "" {
		if cfg.IsProduction() {
			return Config{}, errors.New("UPTIME_BOOTSTRAP_API_KEY must be set in production")
		}
		cfg.BootstrapAPIKey = "dev_admin_key"
	} else if len(cfg.BootstrapAPIKey) < 16 {
		return Config{}, errors.New("UPTIME_BOOTSTRAP_API_KEY must be at least 16 characters")
	}
	if cfg.IsProduction() && cfg.AllowPrivateTargets {
		return Config{}, errors.New("ALLOW_PRIVATE_TARGETS=true is not permitted in production")
	}

	if cfg.ClerkEnabled {
		if cfg.ClerkIssuer == "" {
			return Config{}, errors.New("CLERK_ISSUER must be set when CLERK_ENABLED=true")
		}
		if cfg.ClerkSecretKey == "" {
			return Config{}, errors.New("CLERK_SECRET_KEY must be set when CLERK_ENABLED=true")
		}
		if cfg.ClerkWebhookSecret == "" && cfg.IsProduction() {
			return Config{}, errors.New("CLERK_WEBHOOK_SECRET must be set in production when CLERK_ENABLED=true")
		}
	}

	return cfg, nil
}

// Addr returns the listen address for the API server.
func (c Config) Addr() string { return ":" + c.AppPort }

// MetricsAddr returns the listen address for the worker metrics server.
func (c Config) MetricsAddr() string { return ":" + c.MetricsPort }

// DefaultTimeout returns the default per-check timeout as a Duration.
func (c Config) DefaultTimeout() time.Duration {
	return time.Duration(c.DefaultCheckTimeoutSeconds) * time.Second
}

// ShutdownTimeout returns the maximum duration to wait for in-flight requests
// to complete during graceful shutdown.
func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSeconds) * time.Second
}

// SchedulerTick returns the scheduler poll interval as a Duration.
func (c Config) SchedulerTick() time.Duration {
	return time.Duration(c.SchedulerTickSeconds) * time.Second
}

// WebhookTimeout returns the per-attempt webhook HTTP timeout.
func (c Config) WebhookTimeout() time.Duration {
	return time.Duration(c.WebhookTimeoutSeconds) * time.Second
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("LOG_LEVEL=%q: must be one of debug, info, warn, error", value)
	}
}

func validatePort(value, key string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s=%q is not a valid port: %w", key, value, err)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s=%d must be between 1 and 65535", key, port)
	}
	return nil
}

func validateDatabaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("DATABASE_URL is not a valid URL: %w", err)
	}
	switch u.Scheme {
	case "postgres", "postgresql":
	default:
		return fmt.Errorf("DATABASE_URL scheme %q must be postgres or postgresql", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("DATABASE_URL host is required")
	}
	return nil
}

func validateRedisURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("REDIS_URL is not a valid URL: %w", err)
	}
	switch u.Scheme {
	case "redis", "rediss":
	default:
		return fmt.Errorf("REDIS_URL scheme %q must be redis or rediss", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("REDIS_URL host is required")
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback, fmt.Errorf("%s=%q is not a valid boolean: %w", key, value, err)
	}
	return parsed, nil
}

func getenvInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback, fmt.Errorf("%s=%q is not a valid integer: %w", key, value, err)
	}
	return parsed, nil
}

// splitCSV trims and returns the non-empty entries of a comma-separated list.
// Used for env vars that take multiple values (e.g. CORS_ALLOWED_ORIGINS).
func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenvInt64(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback, fmt.Errorf("%s=%q is not a valid integer: %w", key, value, err)
	}
	return parsed, nil
}
