package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

const incidentColumns = `id, organization_id, monitor_id, status, started_at, resolved_at,
	acknowledged_at, acknowledged_by_user_id, reason,
	last_error, consecutive_failures, created_at, updated_at`

func (s *PostgresStore) ListIncidents(ctx context.Context) ([]models.Incident, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + incidentColumns + ` FROM incidents`
	args := []any{}
	if !skip {
		query += ` WHERE organization_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY started_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	incidents := []models.Incident{}
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return nil, translateError(err)
		}
		incidents = append(incidents, incident)
	}
	return incidents, translateError(rows.Err())
}

func (s *PostgresStore) GetIncident(ctx context.Context, id string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	query := `SELECT ` + incidentColumns + ` FROM incidents WHERE id=$1`
	args := []any{id}
	if !skip {
		query += ` AND organization_id=$2`
		args = append(args, orgID)
	}
	row := s.pool.QueryRow(ctx, query, args...)
	i, err := scanIncident(row)
	return i, translateError(err)
}

// GetOpenIncident returns the currently open incident for a monitor, or nil
// if none exists. A missing row is not an error — it's the common case.
// Scoped via monitor_id (whose FK to monitors already implies an org).
func (s *PostgresStore) GetOpenIncident(ctx context.Context, monitorID string) (*models.Incident, error) {
	if monitorID == "" {
		return nil, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+incidentColumns+`
		FROM incidents WHERE monitor_id=$1 AND status='open'
		ORDER BY started_at DESC LIMIT 1`, monitorID)
	incident, err := scanIncident(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	return &incident, nil
}

// OpenIncident inserts a new incident. The caller must populate
// OrganizationID — typically by copying it from the originating monitor.
func (s *PostgresStore) OpenIncident(ctx context.Context, incident models.Incident) (models.Incident, error) {
	if incident.ID == "" {
		incident.ID = uuid.NewString()
	}
	if incident.StartedAt.IsZero() {
		incident.StartedAt = time.Now().UTC()
	}
	if incident.OrganizationID == "" {
		return models.Incident{}, fmt.Errorf("%w: organization id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO incidents (id, organization_id, monitor_id, status, started_at, reason, last_error, consecutive_failures)
		VALUES ($1,$2,$3,'open',$4,$5,$6,$7)
		RETURNING `+incidentColumns,
		incident.ID, incident.OrganizationID, incident.MonitorID, incident.StartedAt, incident.Reason, incident.LastError, incident.ConsecutiveFailures)
	i, err := scanIncident(row)
	return i, translateError(err)
}

func (s *PostgresStore) ResolveIncident(ctx context.Context, id string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	query := `
		UPDATE incidents SET status='resolved', resolved_at=now(), updated_at=now()
		WHERE id=$1`
	args := []any{id}
	if !skip {
		query += ` AND organization_id=$2`
		args = append(args, orgID)
	}
	query += ` RETURNING ` + incidentColumns
	row := s.pool.QueryRow(ctx, query, args...)
	i, err := scanIncident(row)
	return i, translateError(err)
}

// AcknowledgeIncident records that a user has acknowledged an open incident
// (soft ack — does not pause notifications until on-call escalation lands).
// Re-ack by the same user is a no-op; re-ack by a different user updates
// the user id and timestamp.
func (s *PostgresStore) AcknowledgeIncident(ctx context.Context, id, userID string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	query := `
		UPDATE incidents SET acknowledged_at=now(), acknowledged_by_user_id=$2, updated_at=now()
		WHERE id=$1`
	args := []any{id, nullIfEmpty(userID)}
	if !skip {
		query += ` AND organization_id=$3`
		args = append(args, orgID)
	}
	query += ` RETURNING ` + incidentColumns
	row := s.pool.QueryRow(ctx, query, args...)
	i, err := scanIncident(row)
	return i, translateError(err)
}

func scanIncident(row pgx.Row) (models.Incident, error) {
	var i models.Incident
	var ackUser *string
	err := row.Scan(&i.ID, &i.OrganizationID, &i.MonitorID, &i.Status, &i.StartedAt, &i.ResolvedAt,
		&i.AcknowledgedAt, &ackUser, &i.Reason,
		&i.LastError, &i.ConsecutiveFailures, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return i, err
	}
	if ackUser != nil {
		i.AcknowledgedByUserID = *ackUser
	}
	return i, nil
}
