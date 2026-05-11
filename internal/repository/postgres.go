package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jusso-dev/uptime/internal/models"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func normalizeMonitor(m models.Monitor) models.Monitor {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if m.Method == "" {
		m.Method = "GET"
	}
	if m.ExpectedStatus == 0 && (m.Type == models.MonitorHTTP || m.Type == models.MonitorKeyword) {
		m.ExpectedStatus = 200
	}
	if m.TimeoutSeconds == 0 {
		m.TimeoutSeconds = 10
	}
	if m.IntervalSeconds == 0 {
		m.IntervalSeconds = 60
	}
	if m.FailureThreshold == 0 {
		m.FailureThreshold = 3
	}
	if m.Status == "" {
		m.Status = models.StatusDegraded
	}
	return m
}

func (s *PostgresStore) CreateMonitor(ctx context.Context, monitor models.Monitor) (models.Monitor, error) {
	monitor = normalizeMonitor(monitor)
	row := s.pool.QueryRow(ctx, `
		INSERT INTO monitors (id, name, type, target, method, expected_status, expected_keyword, timeout_seconds, interval_seconds, failure_threshold, enabled, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, name, type, target, method, expected_status, expected_keyword, timeout_seconds, interval_seconds, failure_threshold, enabled, status, created_at, updated_at`,
		monitor.ID, monitor.Name, monitor.Type, monitor.Target, monitor.Method, monitor.ExpectedStatus, monitor.ExpectedKeyword,
		monitor.TimeoutSeconds, monitor.IntervalSeconds, monitor.FailureThreshold, monitor.Enabled, monitor.Status)
	return scanMonitor(row)
}

func (s *PostgresStore) ListMonitors(ctx context.Context) ([]models.Monitor, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, type, target, method, expected_status, expected_keyword, timeout_seconds, interval_seconds, failure_threshold, enabled, status, created_at, updated_at FROM monitors ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMonitors(rows)
}

func (s *PostgresStore) ListEnabledMonitors(ctx context.Context) ([]models.Monitor, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, type, target, method, expected_status, expected_keyword, timeout_seconds, interval_seconds, failure_threshold, enabled, status, created_at, updated_at FROM monitors WHERE enabled = true ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMonitors(rows)
}

func (s *PostgresStore) GetMonitor(ctx context.Context, id string) (models.Monitor, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, name, type, target, method, expected_status, expected_keyword, timeout_seconds, interval_seconds, failure_threshold, enabled, status, created_at, updated_at FROM monitors WHERE id = $1`, id)
	return scanMonitor(row)
}

func (s *PostgresStore) UpdateMonitor(ctx context.Context, monitor models.Monitor) (models.Monitor, error) {
	monitor = normalizeMonitor(monitor)
	row := s.pool.QueryRow(ctx, `
		UPDATE monitors
		SET name=$2, type=$3, target=$4, method=$5, expected_status=$6, expected_keyword=$7, timeout_seconds=$8,
		    interval_seconds=$9, failure_threshold=$10, enabled=$11, updated_at=now()
		WHERE id=$1
		RETURNING id, name, type, target, method, expected_status, expected_keyword, timeout_seconds, interval_seconds, failure_threshold, enabled, status, created_at, updated_at`,
		monitor.ID, monitor.Name, monitor.Type, monitor.Target, monitor.Method, monitor.ExpectedStatus, monitor.ExpectedKeyword,
		monitor.TimeoutSeconds, monitor.IntervalSeconds, monitor.FailureThreshold, monitor.Enabled)
	return scanMonitor(row)
}

func (s *PostgresStore) DeleteMonitor(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM monitors WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) UpdateMonitorStatus(ctx context.Context, id string, status models.CheckStatus) error {
	_, err := s.pool.Exec(ctx, `UPDATE monitors SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	return err
}

func (s *PostgresStore) CreateCheckResult(ctx context.Context, result models.CheckResult) (models.CheckResult, error) {
	if result.ID == "" {
		result.ID = uuid.NewString()
	}
	if result.CheckedAt.IsZero() {
		result.CheckedAt = time.Now().UTC()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO check_results (id, monitor_id, status, success, response_time_ms, status_code, error, checked_at, dns_ms, tcp_connect_ms, tls_handshake_ms, time_to_first_byte_ms, total_ms, response_snippet)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, monitor_id, status, success, response_time_ms, status_code, error, checked_at, dns_ms, tcp_connect_ms, tls_handshake_ms, time_to_first_byte_ms, total_ms, response_snippet`,
		result.ID, result.MonitorID, result.Status, result.Success, result.ResponseTimeMS, result.StatusCode, result.Error,
		result.CheckedAt, result.DNSMS, result.TCPConnectMS, result.TLSHandshakeMS, result.TimeToFirstByteMS, result.TotalMS, result.ResponseSnippet)
	return scanCheckResult(row)
}

func (s *PostgresStore) ListCheckResults(ctx context.Context, filter models.ResultFilter) ([]models.CheckResult, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, monitor_id, status, success, response_time_ms, status_code, error, checked_at, dns_ms, tcp_connect_ms, tls_handshake_ms, time_to_first_byte_ms, total_ms, response_snippet FROM check_results`
	args := []any{}
	clauses := []string{}
	if filter.MonitorID != "" {
		args = append(args, filter.MonitorID)
		clauses = append(clauses, fmt.Sprintf("monitor_id=$%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		clauses = append(clauses, fmt.Sprintf("status=$%d", len(args)))
	}
	if filter.CheckedAfter != nil {
		args = append(args, *filter.CheckedAfter)
		clauses = append(clauses, fmt.Sprintf("checked_at >= $%d", len(args)))
	}
	if filter.CheckedBefore != nil {
		args = append(args, *filter.CheckedBefore)
		clauses = append(clauses, fmt.Sprintf("checked_at <= $%d", len(args)))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, filter.Offset)
	query += fmt.Sprintf(" ORDER BY checked_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []models.CheckResult{}
	for rows.Next() {
		result, err := scanCheckResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (s *PostgresStore) CountConsecutiveFailures(ctx context.Context, monitorID string) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT success FROM check_results WHERE monitor_id=$1 ORDER BY checked_at DESC LIMIT 100`, monitorID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var success bool
		if err := rows.Scan(&success); err != nil {
			return 0, err
		}
		if success {
			break
		}
		count++
	}
	return count, rows.Err()
}

func scanMonitor(row pgx.Row) (models.Monitor, error) {
	var m models.Monitor
	err := row.Scan(&m.ID, &m.Name, &m.Type, &m.Target, &m.Method, &m.ExpectedStatus, &m.ExpectedKeyword, &m.TimeoutSeconds,
		&m.IntervalSeconds, &m.FailureThreshold, &m.Enabled, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func scanMonitors(rows pgx.Rows) ([]models.Monitor, error) {
	monitors := []models.Monitor{}
	for rows.Next() {
		monitor, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, monitor)
	}
	return monitors, rows.Err()
}

func scanCheckResult(row pgx.Row) (models.CheckResult, error) {
	var r models.CheckResult
	err := row.Scan(&r.ID, &r.MonitorID, &r.Status, &r.Success, &r.ResponseTimeMS, &r.StatusCode, &r.Error, &r.CheckedAt,
		&r.DNSMS, &r.TCPConnectMS, &r.TLSHandshakeMS, &r.TimeToFirstByteMS, &r.TotalMS, &r.ResponseSnippet)
	return r, err
}

func ignoreNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
