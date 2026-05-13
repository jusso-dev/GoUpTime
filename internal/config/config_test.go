package config

import (
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppPort != "8008" || cfg.MetricsPort != "8009" {
		t.Fatalf("unexpected default ports: %+v", cfg)
	}
	if cfg.CheckWorkerCount != 10 || cfg.DefaultCheckTimeoutSeconds != 10 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.MaxRequestBodyBytes != 1<<20 {
		t.Fatalf("unexpected body limit: %d", cfg.MaxRequestBodyBytes)
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	clearEnv(t)
	t.Setenv("LOG_LEVEL", "shouty")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("expected LOG_LEVEL error, got %v", err)
	}
}

func TestLoadRejectsOutOfRangeWorkerCount(t *testing.T) {
	clearEnv(t)
	t.Setenv("CHECK_WORKER_COUNT", "0")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CHECK_WORKER_COUNT") {
		t.Fatalf("expected CHECK_WORKER_COUNT error, got %v", err)
	}
}

func TestLoadRejectsNonNumericInt(t *testing.T) {
	clearEnv(t)
	t.Setenv("CHECK_WORKER_COUNT", "nope")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "valid integer") {
		t.Fatalf("expected integer parse error, got %v", err)
	}
}

func TestLoadRejectsDuplicatePorts(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_PORT", "9000")
	t.Setenv("METRICS_PORT", "9000")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected duplicate port error, got %v", err)
	}
}

func TestLoadRejectsInvalidDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "mysql://u:p@h/db")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
}

func TestProductionRequiresBootstrapKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "production")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BOOTSTRAP_API_KEY") {
		t.Fatalf("expected bootstrap key required, got %v", err)
	}
}

func TestProductionBlocksPrivateTargets(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("UPTIME_BOOTSTRAP_API_KEY", "a-strong-bootstrap-key-32chars-ok")
	t.Setenv("ALLOW_PRIVATE_TARGETS", "true")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ALLOW_PRIVATE_TARGETS") {
		t.Fatalf("expected ALLOW_PRIVATE_TARGETS error, got %v", err)
	}
}

// clearEnv unsets every Config-backed environment variable so each test
// sees a clean slate independent of the caller's shell.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_ENV", "APP_PORT", "METRICS_PORT", "APP_VERSION",
		"DATABASE_URL", "REDIS_URL", "UPTIME_BOOTSTRAP_API_KEY",
		"ALLOW_PRIVATE_TARGETS", "CHECK_WORKER_COUNT", "DEFAULT_CHECK_TIMEOUT_SECONDS",
		"LOG_LEVEL", "HTTP_USER_AGENT", "TLS_EXPIRY_WARN_DAYS",
		"WEBHOOK_SIGNING_SECRET", "WEBHOOK_TIMEOUT_SECONDS", "WEBHOOK_MAX_RETRIES",
		"SHUTDOWN_TIMEOUT_SECONDS", "API_READ_HEADER_TIMEOUT_SECONDS",
		"API_WRITE_TIMEOUT_SECONDS", "MAX_REQUEST_BODY_BYTES", "SCHEDULER_TICK_SECONDS",
	} {
		t.Setenv(key, "")
	}
}
