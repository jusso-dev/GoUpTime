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

const maintenanceColumns = `id, organization_id, name, description, starts_at, ends_at,
	recurrence_rrule, status_page_id, created_by_user_id, created_at, updated_at`

func (s *PostgresStore) ListMaintenanceWindows(ctx context.Context) ([]models.MaintenanceWindow, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + maintenanceColumns + ` FROM maintenance_windows`
	args := []any{}
	if !skip {
		query += ` WHERE organization_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY starts_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	windows := []models.MaintenanceWindow{}
	for rows.Next() {
		w, err := scanMaintenance(rows)
		if err != nil {
			return nil, translateError(err)
		}
		w.MonitorIDs, _ = s.maintenanceMonitorIDs(ctx, w.ID)
		windows = append(windows, w)
	}
	return windows, translateError(rows.Err())
}

func (s *PostgresStore) CreateMaintenanceWindow(ctx context.Context, w models.MaintenanceWindow) (models.MaintenanceWindow, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.MaintenanceWindow{}, err
	}
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	w.OrganizationID = orgID
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.MaintenanceWindow{}, translateError(err)
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		INSERT INTO maintenance_windows (id, organization_id, name, description, starts_at, ends_at,
			recurrence_rrule, status_page_id, created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+maintenanceColumns,
		w.ID, orgID, w.Name, w.Description, w.StartsAt, w.EndsAt,
		w.RecurrenceRRule, nullIfEmpty(w.StatusPageID), nullIfEmpty(w.CreatedByUserID))
	saved, err := scanMaintenance(row)
	if err != nil {
		return saved, translateError(err)
	}
	for _, mid := range w.MonitorIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO maintenance_window_monitors (maintenance_window_id, monitor_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, saved.ID, mid); err != nil {
			return saved, translateError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return saved, translateError(err)
	}
	saved.MonitorIDs = w.MonitorIDs
	return saved, nil
}

func (s *PostgresStore) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM maintenance_windows WHERE id=$1 AND organization_id=$2`, id, orgID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

// IsMonitorInMaintenance returns true if any non-expired window
// covering the given monitor is active at `at`. Used by the scheduler
// before enqueuing a check so suppression is enforced before we waste
// network and storage on an expected failure.
//
// Recurrence: today we only evaluate the literal starts_at/ends_at
// columns. RRULE expansion lands in a follow-up; document a one-shot
// "next occurrence" cron entry as the interim pattern.
func (s *PostgresStore) IsMonitorInMaintenance(ctx context.Context, monitorID string, at time.Time) (bool, error) {
	if monitorID == "" {
		return false, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM maintenance_windows w
			JOIN maintenance_window_monitors m ON m.maintenance_window_id = w.id
			WHERE m.monitor_id = $1
			  AND w.starts_at <= $2
			  AND w.ends_at >= $2
		)`, monitorID, at).Scan(&exists)
	if err != nil {
		return false, translateError(err)
	}
	return exists, nil
}

func (s *PostgresStore) maintenanceMonitorIDs(ctx context.Context, windowID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT monitor_id FROM maintenance_window_monitors WHERE maintenance_window_id=$1`, windowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanMaintenance(row pgx.Row) (models.MaintenanceWindow, error) {
	var w models.MaintenanceWindow
	var statusPageID, createdBy *string
	err := row.Scan(&w.ID, &w.OrganizationID, &w.Name, &w.Description, &w.StartsAt, &w.EndsAt,
		&w.RecurrenceRRule, &statusPageID, &createdBy, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, apierr.ErrNotFound
	}
	if err != nil {
		return w, err
	}
	if statusPageID != nil {
		w.StatusPageID = *statusPageID
	}
	if createdBy != nil {
		w.CreatedByUserID = *createdBy
	}
	return w, nil
}
