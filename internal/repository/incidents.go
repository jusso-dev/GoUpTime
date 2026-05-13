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

const incidentColumns = `id, monitor_id, status, started_at, resolved_at, reason,
	last_error, consecutive_failures, created_at, updated_at`

func (s *PostgresStore) ListIncidents(ctx context.Context) ([]models.Incident, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+incidentColumns+` FROM incidents ORDER BY started_at DESC`)
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
	row := s.pool.QueryRow(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE id=$1`, id)
	i, err := scanIncident(row)
	return i, translateError(err)
}

// GetOpenIncident returns the currently open incident for a monitor, or nil
// if none exists. A missing row is not an error — it's the common case.
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

func (s *PostgresStore) OpenIncident(ctx context.Context, incident models.Incident) (models.Incident, error) {
	if incident.ID == "" {
		incident.ID = uuid.NewString()
	}
	if incident.StartedAt.IsZero() {
		incident.StartedAt = time.Now().UTC()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO incidents (id, monitor_id, status, started_at, reason, last_error, consecutive_failures)
		VALUES ($1,$2,'open',$3,$4,$5,$6)
		RETURNING `+incidentColumns,
		incident.ID, incident.MonitorID, incident.StartedAt, incident.Reason, incident.LastError, incident.ConsecutiveFailures)
	i, err := scanIncident(row)
	return i, translateError(err)
}

func (s *PostgresStore) ResolveIncident(ctx context.Context, id string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE incidents SET status='resolved', resolved_at=now(), updated_at=now()
		WHERE id=$1
		RETURNING `+incidentColumns, id)
	i, err := scanIncident(row)
	return i, translateError(err)
}

func scanIncident(row pgx.Row) (models.Incident, error) {
	var i models.Incident
	err := row.Scan(&i.ID, &i.MonitorID, &i.Status, &i.StartedAt, &i.ResolvedAt, &i.Reason,
		&i.LastError, &i.ConsecutiveFailures, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}
