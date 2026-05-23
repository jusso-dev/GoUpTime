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

const outboxColumns = `id, organization_id, channel_id, incident_id, event_type, payload,
	attempts, next_attempt_at, status, last_error, created_at, updated_at`

// EnqueueNotification writes a pending outbox row. Called inside the
// service tx around an incident state change so a crash between commit
// and dispatch can't drop the alert.
func (s *PostgresStore) EnqueueNotification(ctx context.Context, entry models.OutboxEntry) (models.OutboxEntry, error) {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.OrganizationID == "" {
		return entry, fmt.Errorf("%w: organization id is required", apierr.ErrInvalidInput)
	}
	if entry.Status == "" {
		entry.Status = "pending"
	}
	if entry.NextAttemptAt.IsZero() {
		entry.NextAttemptAt = time.Now().UTC()
	}
	if entry.Payload == nil {
		entry.Payload = []byte("{}")
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO notification_outbox (id, organization_id, channel_id, incident_id, event_type,
			payload, attempts, next_attempt_at, status, last_error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+outboxColumns,
		entry.ID, entry.OrganizationID, nullIfEmpty(entry.ChannelID), nullIfEmpty(entry.IncidentID),
		entry.EventType, entry.Payload, entry.Attempts, entry.NextAttemptAt, entry.Status, entry.LastError)
	return scanOutboxEntry(row)
}

// ClaimPendingNotifications selects up to `limit` pending entries whose
// next_attempt_at is in the past and locks them for the calling
// transaction. Using FOR UPDATE SKIP LOCKED lets multiple poller
// replicas drain the queue concurrently without stepping on each other.
//
// Callers must Commit or Rollback the returned transaction; on commit
// the rows remain locked only briefly because each claim is one row.
type ClaimedEntry struct {
	Entry models.OutboxEntry
}

func (s *PostgresStore) ClaimPendingNotifications(ctx context.Context, limit int) ([]models.OutboxEntry, pgx.Tx, error) {
	if limit <= 0 {
		limit = 50
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT `+outboxColumns+`
		FROM notification_outbox
		WHERE status = 'pending' AND next_attempt_at <= now()
		ORDER BY next_attempt_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, translateError(err)
	}
	entries := []models.OutboxEntry{}
	for rows.Next() {
		entry, err := scanOutboxEntry(rows)
		if err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			return nil, nil, translateError(err)
		}
		entries = append(entries, entry)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, translateError(err)
	}
	return entries, tx, nil
}

// MarkNotificationDelivered must be called via the tx returned by
// ClaimPendingNotifications so the row's lock is released only on commit.
func (s *PostgresStore) MarkNotificationDelivered(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `
		UPDATE notification_outbox
		SET status = 'delivered', updated_at = now()
		WHERE id = $1`, id)
	return translateError(err)
}

// MarkNotificationRetry bumps the attempt counter, records the last
// error, and schedules the next attempt. After maxAttempts the row is
// marked failed so the poller doesn't loop forever.
func (s *PostgresStore) MarkNotificationRetry(ctx context.Context, tx pgx.Tx, id string, attempts, maxAttempts int, lastErr string, next time.Time) error {
	status := "pending"
	if attempts >= maxAttempts {
		status = "failed"
	}
	_, err := tx.Exec(ctx, `
		UPDATE notification_outbox
		SET attempts = $2, next_attempt_at = $3, status = $4, last_error = $5, updated_at = now()
		WHERE id = $1`,
		id, attempts, next, status, lastErr)
	return translateError(err)
}

// --- push devices ----------------------------------------------------

const pushDeviceColumns = `id, organization_id, user_id, platform, expo_token,
	app_version, last_seen_at, created_at`

func (s *PostgresStore) UpsertPushDevice(ctx context.Context, device models.PushDevice) (models.PushDevice, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.PushDevice{}, err
	}
	if device.ID == "" {
		device.ID = uuid.NewString()
	}
	device.OrganizationID = orgID
	row := s.pool.QueryRow(ctx, `
		INSERT INTO push_devices (id, organization_id, user_id, platform, expo_token, app_version, last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (expo_token) DO UPDATE SET
			organization_id = EXCLUDED.organization_id,
			user_id = EXCLUDED.user_id,
			platform = EXCLUDED.platform,
			app_version = EXCLUDED.app_version,
			last_seen_at = now()
		RETURNING `+pushDeviceColumns,
		device.ID, device.OrganizationID, device.UserID, device.Platform, device.ExpoToken, device.AppVersion)
	return scanPushDevice(row)
}

func (s *PostgresStore) DeletePushDevice(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: device id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM push_devices WHERE id=$1 AND organization_id=$2`, id, orgID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListPushDevicesForOrg(ctx context.Context, organizationID string) ([]models.PushDevice, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+pushDeviceColumns+`
		FROM push_devices WHERE organization_id = $1
		ORDER BY last_seen_at DESC`, organizationID)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	devices := []models.PushDevice{}
	for rows.Next() {
		d, err := scanPushDevice(rows)
		if err != nil {
			return nil, translateError(err)
		}
		devices = append(devices, d)
	}
	return devices, translateError(rows.Err())
}

func (s *PostgresStore) ListPushDevicesForUser(ctx context.Context, userID string) ([]models.PushDevice, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + pushDeviceColumns + ` FROM push_devices WHERE user_id = $1`
	args := []any{userID}
	if !skip {
		query += ` AND organization_id = $2`
		args = append(args, orgID)
	}
	query += ` ORDER BY last_seen_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	devices := []models.PushDevice{}
	for rows.Next() {
		d, err := scanPushDevice(rows)
		if err != nil {
			return nil, translateError(err)
		}
		devices = append(devices, d)
	}
	return devices, translateError(rows.Err())
}

func scanOutboxEntry(row pgx.Row) (models.OutboxEntry, error) {
	var e models.OutboxEntry
	var channelID, incidentID *string
	err := row.Scan(&e.ID, &e.OrganizationID, &channelID, &incidentID, &e.EventType, &e.Payload,
		&e.Attempts, &e.NextAttemptAt, &e.Status, &e.LastError, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, apierr.ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if channelID != nil {
		e.ChannelID = *channelID
	}
	if incidentID != nil {
		e.IncidentID = *incidentID
	}
	return e, nil
}

func scanPushDevice(row pgx.Row) (models.PushDevice, error) {
	var d models.PushDevice
	err := row.Scan(&d.ID, &d.OrganizationID, &d.UserID, &d.Platform, &d.ExpoToken,
		&d.AppVersion, &d.LastSeenAt, &d.CreatedAt)
	return d, err
}
