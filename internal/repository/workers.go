package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

const workerHeartbeatColumns = `instance_id, hostname, version, region, started_at, last_seen_at,
	worker_count, active_jobs, queue_depth, queue_capacity, jobs_completed, jobs_failed, in_flight`

// UpsertWorkerHeartbeat inserts a fresh row for the calling worker or
// updates the existing one in-place. last_seen_at is always bumped so the
// API can detect stale entries without a separate "alive?" probe.
func (s *PostgresStore) UpsertWorkerHeartbeat(ctx context.Context, hb models.WorkerHeartbeat) error {
	if hb.InstanceID == "" {
		return fmt.Errorf("%w: instance id is required", apierr.ErrInvalidInput)
	}
	if hb.InFlight == nil {
		hb.InFlight = []string{}
	}
	inFlightJSON, err := json.Marshal(hb.InFlight)
	if err != nil {
		return fmt.Errorf("marshal in-flight ids: %w", err)
	}
	if hb.LastSeenAt.IsZero() {
		hb.LastSeenAt = time.Now().UTC()
	}
	if hb.Region == "" {
		hb.Region = "default"
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO worker_heartbeats (`+workerHeartbeatColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (instance_id) DO UPDATE SET
			hostname        = EXCLUDED.hostname,
			version         = EXCLUDED.version,
			region          = EXCLUDED.region,
			last_seen_at    = EXCLUDED.last_seen_at,
			worker_count    = EXCLUDED.worker_count,
			active_jobs     = EXCLUDED.active_jobs,
			queue_depth     = EXCLUDED.queue_depth,
			queue_capacity  = EXCLUDED.queue_capacity,
			jobs_completed  = EXCLUDED.jobs_completed,
			jobs_failed     = EXCLUDED.jobs_failed,
			in_flight       = EXCLUDED.in_flight`,
		hb.InstanceID, hb.Hostname, hb.Version, hb.Region, hb.StartedAt, hb.LastSeenAt,
		hb.WorkerCount, hb.ActiveJobs, hb.QueueDepth, hb.QueueCapacity,
		hb.JobsCompleted, hb.JobsFailed, inFlightJSON)
	return translateError(err)
}

// ListWorkerHeartbeats returns every heartbeat whose last_seen_at is >= since.
// A zero-valued since returns all rows. The caller decides what counts as
// "stale" — the repository does not filter that out.
func (s *PostgresStore) ListWorkerHeartbeats(ctx context.Context, since time.Time) ([]models.WorkerHeartbeat, error) {
	args := []any{}
	query := `SELECT ` + workerHeartbeatColumns + ` FROM worker_heartbeats`
	if !since.IsZero() {
		query += ` WHERE last_seen_at >= $1`
		args = append(args, since)
	}
	query += ` ORDER BY last_seen_at DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	beats := []models.WorkerHeartbeat{}
	for rows.Next() {
		hb, err := scanWorkerHeartbeat(rows)
		if err != nil {
			return nil, translateError(err)
		}
		beats = append(beats, hb)
	}
	return beats, translateError(rows.Err())
}

// DeleteWorkerHeartbeat removes a worker's row. Workers call this on clean
// shutdown so the UI doesn't briefly show them as alive after a deploy.
// A missing row is not an error — it can happen if the heartbeat was never
// written or has already been purged.
func (s *PostgresStore) DeleteWorkerHeartbeat(ctx context.Context, instanceID string) error {
	if instanceID == "" {
		return fmt.Errorf("%w: instance id is required", apierr.ErrInvalidInput)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM worker_heartbeats WHERE instance_id=$1`, instanceID)
	return translateError(err)
}

func scanWorkerHeartbeat(row pgx.Row) (models.WorkerHeartbeat, error) {
	var hb models.WorkerHeartbeat
	var inFlightJSON []byte
	err := row.Scan(
		&hb.InstanceID, &hb.Hostname, &hb.Version, &hb.Region, &hb.StartedAt, &hb.LastSeenAt,
		&hb.WorkerCount, &hb.ActiveJobs, &hb.QueueDepth, &hb.QueueCapacity,
		&hb.JobsCompleted, &hb.JobsFailed, &inFlightJSON,
	)
	if err != nil {
		return hb, err
	}
	if len(inFlightJSON) > 0 {
		if err := json.Unmarshal(inFlightJSON, &hb.InFlight); err != nil {
			return hb, fmt.Errorf("decode in-flight ids: %w", err)
		}
	}
	if hb.InFlight == nil {
		hb.InFlight = []string{}
	}
	return hb, nil
}
