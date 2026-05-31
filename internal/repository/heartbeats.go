package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

// Heartbeats are scoped to a single monitor via PK; the monitor_id FK
// implicitly enforces tenancy because monitors carry organization_id.
// These methods therefore don't need explicit tenant filtering, but the
// SetHeartbeat/DeleteHeartbeat callers should still pre-verify the
// monitor belongs to the requesting org.

const heartbeatColumns = `monitor_id, token_hash, expected_interval_seconds, grace_seconds,
	last_ping_at, last_ping_source_ip, last_ping_user_agent, created_at, updated_at`

func (s *PostgresStore) GetHeartbeat(ctx context.Context, monitorID string) (models.Heartbeat, error) {
	if monitorID == "" {
		return models.Heartbeat{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+heartbeatColumns+` FROM heartbeats WHERE monitor_id=$1`, monitorID)
	return scanHeartbeat(row)
}

// SetHeartbeat inserts or updates the heartbeat row for a monitor.
// Returns ErrNotFound if monitorID does not exist (FK violation).
func (s *PostgresStore) SetHeartbeat(ctx context.Context, hb models.Heartbeat) (models.Heartbeat, error) {
	if hb.MonitorID == "" {
		return models.Heartbeat{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	if hb.ExpectedIntervalSeconds <= 0 {
		hb.ExpectedIntervalSeconds = 60
	}
	if hb.GraceSeconds < 0 {
		hb.GraceSeconds = 0
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO heartbeats (monitor_id, token_hash, expected_interval_seconds, grace_seconds)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (monitor_id) DO UPDATE SET
			token_hash = EXCLUDED.token_hash,
			expected_interval_seconds = EXCLUDED.expected_interval_seconds,
			grace_seconds = EXCLUDED.grace_seconds,
			updated_at = now()
		RETURNING `+heartbeatColumns,
		hb.MonitorID, hb.TokenHash, hb.ExpectedIntervalSeconds, hb.GraceSeconds)
	return scanHeartbeat(row)
}

func (s *PostgresStore) DeleteHeartbeat(ctx context.Context, monitorID string) error {
	if monitorID == "" {
		return fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM heartbeats WHERE monitor_id=$1`, monitorID)
	return translateError(err)
}

// RecordHeartbeatPing updates the last-seen metadata for the heartbeat
// whose token matches tokenHash. Returns the monitor id on success so
// the caller can rate-limit per-monitor and log structured events.
// Returns ErrNotFound when no heartbeat matches.
func (s *PostgresStore) RecordHeartbeatPing(ctx context.Context, tokenHash, sourceIP, userAgent string) (string, error) {
	if tokenHash == "" {
		return "", fmt.Errorf("%w: token is required", apierr.ErrInvalidInput)
	}
	var monitorID string
	err := s.pool.QueryRow(ctx, `
		UPDATE heartbeats
		SET last_ping_at = now(),
		    last_ping_source_ip = $2,
		    last_ping_user_agent = $3,
		    updated_at = now()
		WHERE token_hash = $1
		RETURNING monitor_id`,
		tokenHash, sourceIP, userAgent).Scan(&monitorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apierr.ErrNotFound
	}
	if err != nil {
		return "", translateError(err)
	}
	return monitorID, nil
}

func scanHeartbeat(row pgx.Row) (models.Heartbeat, error) {
	var hb models.Heartbeat
	err := row.Scan(&hb.MonitorID, &hb.TokenHash, &hb.ExpectedIntervalSeconds, &hb.GraceSeconds,
		&hb.LastPingAt, &hb.LastPingSourceIP, &hb.LastPingUserAgent, &hb.CreatedAt, &hb.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return hb, apierr.ErrNotFound
	}
	return hb, translateError(err)
}

// --- multistep & browser scripts ----------------------------------------

func (s *PostgresStore) GetMultistepScript(ctx context.Context, monitorID string) (models.MultistepScript, error) {
	if monitorID == "" {
		return models.MultistepScript{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	var script models.MultistepScript
	var stepsJSON []byte
	err := s.pool.QueryRow(ctx, `SELECT monitor_id, steps, created_at, updated_at
		FROM multistep_scripts WHERE monitor_id=$1`, monitorID).
		Scan(&script.MonitorID, &stepsJSON, &script.CreatedAt, &script.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return script, apierr.ErrNotFound
	}
	if err != nil {
		return script, translateError(err)
	}
	if len(stepsJSON) > 0 {
		if err := json.Unmarshal(stepsJSON, &script.Steps); err != nil {
			return script, fmt.Errorf("decode multistep script: %w", err)
		}
	}
	return script, nil
}

func (s *PostgresStore) SetMultistepScript(ctx context.Context, script models.MultistepScript) (models.MultistepScript, error) {
	if script.MonitorID == "" {
		return models.MultistepScript{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	stepsJSON, err := json.Marshal(script.Steps)
	if err != nil {
		return script, fmt.Errorf("encode multistep script: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO multistep_scripts (monitor_id, steps)
		VALUES ($1, $2)
		ON CONFLICT (monitor_id) DO UPDATE SET
			steps = EXCLUDED.steps,
			updated_at = now()`,
		script.MonitorID, stepsJSON)
	if err != nil {
		return script, translateError(err)
	}
	return s.GetMultistepScript(ctx, script.MonitorID)
}

func (s *PostgresStore) GetBrowserScript(ctx context.Context, monitorID string) (models.BrowserScript, error) {
	if monitorID == "" {
		return models.BrowserScript{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	var script models.BrowserScript
	err := s.pool.QueryRow(ctx, `SELECT monitor_id, source, created_at, updated_at
		FROM browser_scripts WHERE monitor_id=$1`, monitorID).
		Scan(&script.MonitorID, &script.Source, &script.CreatedAt, &script.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return script, apierr.ErrNotFound
	}
	return script, translateError(err)
}

func (s *PostgresStore) SetBrowserScript(ctx context.Context, script models.BrowserScript) (models.BrowserScript, error) {
	if script.MonitorID == "" {
		return models.BrowserScript{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO browser_scripts (monitor_id, source)
		VALUES ($1, $2)
		ON CONFLICT (monitor_id) DO UPDATE SET
			source = EXCLUDED.source,
			updated_at = now()`,
		script.MonitorID, script.Source)
	if err != nil {
		return script, translateError(err)
	}
	return s.GetBrowserScript(ctx, script.MonitorID)
}

// scanCheckResult — we need to extend the scanner used by the existing
// CheckResult queries to also read domain_expires_at. The existing
// checkResultColumns const must be updated alongside.
//
// Time delay: the existing scanCheckResult lives in postgres.go and
// already reads 15 columns; updating it lives there to keep the column
// list and scanner together.
var _ = time.Time{}
