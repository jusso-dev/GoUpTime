package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

// PostgresStore is a thin pgxpool-backed implementation of Store. All public
// methods accept a context and propagate it to the underlying driver so
// request cancellation and deadlines work end-to-end.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// PoolConfig tunes the connection pool. Zero values fall back to sensible
// defaults; callers should normally override MaxConns for production.
type PoolConfig struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
}

// DefaultPoolConfig returns conservative defaults suitable for a single
// medium-sized app instance. Operators should tune MaxConns based on Postgres
// max_connections and the number of API/worker replicas they run.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConns:          20,
		MinConns:          2,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   5 * time.Minute,
		HealthCheckPeriod: 1 * time.Minute,
	}
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Open parses databaseURL, applies pool tuning, opens the pool, and verifies
// connectivity with a bounded ping. The caller owns the returned pool and is
// responsible for Close.
func Open(ctx context.Context, databaseURL string, pc PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if pc.MaxConns > 0 {
		cfg.MaxConns = pc.MaxConns
	}
	if pc.MinConns > 0 {
		cfg.MinConns = pc.MinConns
	}
	if pc.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = pc.MaxConnLifetime
	}
	if pc.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = pc.MaxConnIdleTime
	}
	if pc.HealthCheckPeriod > 0 {
		cfg.HealthCheckPeriod = pc.HealthCheckPeriod
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// translateError normalizes driver errors to sentinel apierr values so
// handlers can map them to HTTP statuses without importing pgx. Returns nil
// for nil input.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", apierr.ErrConflict, pgErr.ConstraintName)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: foreign key %s", apierr.ErrInvalidInput, pgErr.ConstraintName)
		case "23514": // check_violation
			return fmt.Errorf("%w: check %s failed", apierr.ErrInvalidInput, pgErr.ConstraintName)
		case "22P02": // invalid_text_representation (e.g. malformed uuid)
			return fmt.Errorf("%w: %s", apierr.ErrInvalidInput, pgErr.Message)
		}
	}
	return err
}

const (
	monitorColumns = `id, organization_id, name, type, target, method, expected_status, expected_keyword,
		timeout_seconds, interval_seconds, failure_threshold, enabled, status, created_at, updated_at`

	checkResultColumns = `id, organization_id, monitor_id, status, success, response_time_ms, status_code, error,
		checked_at, dns_ms, tcp_connect_ms, tls_handshake_ms, time_to_first_byte_ms, total_ms, response_snippet`
)

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
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Monitor{}, err
	}
	monitor = normalizeMonitor(monitor)
	monitor.OrganizationID = orgID
	row := s.pool.QueryRow(ctx, `
		INSERT INTO monitors (id, organization_id, name, type, target, method, expected_status, expected_keyword, timeout_seconds, interval_seconds, failure_threshold, enabled, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+monitorColumns,
		monitor.ID, monitor.OrganizationID, monitor.Name, monitor.Type, monitor.Target, monitor.Method, monitor.ExpectedStatus, monitor.ExpectedKeyword,
		monitor.TimeoutSeconds, monitor.IntervalSeconds, monitor.FailureThreshold, monitor.Enabled, monitor.Status)
	m, err := scanMonitor(row)
	return m, translateError(err)
}

func (s *PostgresStore) ListMonitors(ctx context.Context) ([]models.Monitor, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + monitorColumns + ` FROM monitors`
	args := []any{}
	if !skip {
		query += ` WHERE organization_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	monitors, err := scanMonitors(rows)
	return monitors, translateError(err)
}

// ListEnabledMonitors is always called by the worker scheduler and therefore
// crosses tenants. Callers must use auth.WithSystem(ctx) — the tenancy
// scope helper enforces this by allowing skip only for system actors.
func (s *PostgresStore) ListEnabledMonitors(ctx context.Context) ([]models.Monitor, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + monitorColumns + ` FROM monitors WHERE enabled = true`
	args := []any{}
	if !skip {
		query += ` AND organization_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY created_at`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	monitors, err := scanMonitors(rows)
	return monitors, translateError(err)
}

func (s *PostgresStore) GetMonitor(ctx context.Context, id string) (models.Monitor, error) {
	if id == "" {
		return models.Monitor{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Monitor{}, err
	}
	query := `SELECT ` + monitorColumns + ` FROM monitors WHERE id = $1`
	args := []any{id}
	if !skip {
		query += ` AND organization_id = $2`
		args = append(args, orgID)
	}
	row := s.pool.QueryRow(ctx, query, args...)
	m, err := scanMonitor(row)
	return m, translateError(err)
}

func (s *PostgresStore) UpdateMonitor(ctx context.Context, monitor models.Monitor) (models.Monitor, error) {
	if monitor.ID == "" {
		return models.Monitor{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Monitor{}, err
	}
	monitor = normalizeMonitor(monitor)
	row := s.pool.QueryRow(ctx, `
		UPDATE monitors
		SET name=$3, type=$4, target=$5, method=$6, expected_status=$7, expected_keyword=$8, timeout_seconds=$9,
		    interval_seconds=$10, failure_threshold=$11, enabled=$12, updated_at=now()
		WHERE id=$1 AND organization_id=$2
		RETURNING `+monitorColumns,
		monitor.ID, orgID, monitor.Name, monitor.Type, monitor.Target, monitor.Method, monitor.ExpectedStatus, monitor.ExpectedKeyword,
		monitor.TimeoutSeconds, monitor.IntervalSeconds, monitor.FailureThreshold, monitor.Enabled)
	m, err := scanMonitor(row)
	return m, translateError(err)
}

func (s *PostgresStore) DeleteMonitor(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM monitors WHERE id=$1 AND organization_id=$2`, id, orgID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) UpdateMonitorStatus(ctx context.Context, id string, status models.CheckStatus) error {
	if id == "" {
		return fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return err
	}
	query := `UPDATE monitors SET status=$2, updated_at=now() WHERE id=$1`
	args := []any{id, status}
	if !skip {
		query += ` AND organization_id=$3`
		args = append(args, orgID)
	}
	_, err = s.pool.Exec(ctx, query, args...)
	return translateError(err)
}

func (s *PostgresStore) CreateCheckResult(ctx context.Context, result models.CheckResult) (models.CheckResult, error) {
	if result.ID == "" {
		result.ID = uuid.NewString()
	}
	if result.CheckedAt.IsZero() {
		result.CheckedAt = time.Now().UTC()
	}
	if result.OrganizationID == "" {
		// Fall back to the principal's org if the caller didn't set it
		// explicitly — happens when the service layer doesn't know which
		// monitor the result belongs to (rare).
		orgID, err := s.requireOrg(ctx)
		if err != nil {
			return models.CheckResult{}, err
		}
		result.OrganizationID = orgID
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO check_results (`+checkResultColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING `+checkResultColumns,
		result.ID, result.OrganizationID, result.MonitorID, result.Status, result.Success, result.ResponseTimeMS, result.StatusCode, result.Error,
		result.CheckedAt, result.DNSMS, result.TCPConnectMS, result.TLSHandshakeMS, result.TimeToFirstByteMS, result.TotalMS, result.ResponseSnippet)
	r, err := scanCheckResult(row)
	return r, translateError(err)
}

func (s *PostgresStore) ListCheckResults(ctx context.Context, filter models.ResultFilter) ([]models.CheckResult, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + checkResultColumns + ` FROM check_results`
	args := []any{}
	clauses := []string{}
	if !skip {
		args = append(args, orgID)
		clauses = append(clauses, fmt.Sprintf("organization_id=$%d", len(args)))
	}
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
		return nil, translateError(err)
	}
	defer rows.Close()
	results := []models.CheckResult{}
	for rows.Next() {
		result, err := scanCheckResult(rows)
		if err != nil {
			return nil, translateError(err)
		}
		results = append(results, result)
	}
	return results, translateError(rows.Err())
}

// CountConsecutiveFailures walks back through recent check results until it
// hits a success. A small limit (50) bounds the work and is sufficient for
// any realistic failure_threshold. Filtering by monitor_id implicitly
// scopes results to the monitor's org (check_results.monitor_id is a FK)
// so no explicit org filter is needed here.
func (s *PostgresStore) CountConsecutiveFailures(ctx context.Context, monitorID string) (int, error) {
	if monitorID == "" {
		return 0, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	rows, err := s.pool.Query(ctx, `SELECT success FROM check_results WHERE monitor_id=$1 ORDER BY checked_at DESC LIMIT 50`, monitorID)
	if err != nil {
		return 0, translateError(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var success bool
		if err := rows.Scan(&success); err != nil {
			return 0, translateError(err)
		}
		if success {
			break
		}
		count++
	}
	return count, translateError(rows.Err())
}

func scanMonitor(row pgx.Row) (models.Monitor, error) {
	var m models.Monitor
	err := row.Scan(&m.ID, &m.OrganizationID, &m.Name, &m.Type, &m.Target, &m.Method, &m.ExpectedStatus, &m.ExpectedKeyword, &m.TimeoutSeconds,
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
	err := row.Scan(&r.ID, &r.OrganizationID, &r.MonitorID, &r.Status, &r.Success, &r.ResponseTimeMS, &r.StatusCode, &r.Error, &r.CheckedAt,
		&r.DNSMS, &r.TCPConnectMS, &r.TLSHandshakeMS, &r.TimeToFirstByteMS, &r.TotalMS, &r.ResponseSnippet)
	return r, err
}
