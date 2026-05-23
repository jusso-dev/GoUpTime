package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

const notificationChannelColumns = `id, organization_id, name, type, url, config, enabled, created_at, updated_at`

func (s *PostgresStore) ListNotificationChannels(ctx context.Context) ([]models.NotificationChannel, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + notificationChannelColumns + ` FROM notification_channels`
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
	channels := []models.NotificationChannel{}
	for rows.Next() {
		channel, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, translateError(err)
		}
		channels = append(channels, channel)
	}
	return channels, translateError(rows.Err())
}

func (s *PostgresStore) GetNotificationChannel(ctx context.Context, id string) (models.NotificationChannel, error) {
	if id == "" {
		return models.NotificationChannel{}, fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	query := `SELECT ` + notificationChannelColumns + ` FROM notification_channels WHERE id=$1`
	args := []any{id}
	if !skip {
		query += ` AND organization_id=$2`
		args = append(args, orgID)
	}
	row := s.pool.QueryRow(ctx, query, args...)
	c, err := scanNotificationChannel(row)
	return c, translateError(err)
}

func (s *PostgresStore) CreateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	if channel.ID == "" {
		channel.ID = uuid.NewString()
	}
	channel.OrganizationID = orgID
	configJSON, err := marshalConfig(channel.Config)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO notification_channels (id, organization_id, name, type, url, config, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+notificationChannelColumns,
		channel.ID, channel.OrganizationID, channel.Name, channel.Type, channel.URL, configJSON, channel.Enabled)
	c, err := scanNotificationChannel(row)
	return c, translateError(err)
}

func (s *PostgresStore) UpdateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error) {
	if channel.ID == "" {
		return models.NotificationChannel{}, fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	configJSON, err := marshalConfig(channel.Config)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE notification_channels SET name=$3, type=$4, url=$5, config=$6, enabled=$7, updated_at=now()
		WHERE id=$1 AND organization_id=$2
		RETURNING `+notificationChannelColumns,
		channel.ID, orgID, channel.Name, channel.Type, channel.URL, configJSON, channel.Enabled)
	c, err := scanNotificationChannel(row)
	return c, translateError(err)
}

func (s *PostgresStore) DeleteNotificationChannel(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM notification_channels WHERE id=$1 AND organization_id=$2`, id, orgID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

// LogNotificationEvent appends an audit row. It is intentionally not
// org-scoped at the query level — channel_id and incident_id already imply
// the org and this table is opaque to end-users (no list endpoint).
func (s *PostgresStore) LogNotificationEvent(ctx context.Context, channelID, incidentID, eventType string, success bool, statusCode int, errText string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO notification_events (id, channel_id, incident_id, event_type, success, status_code, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		uuid.NewString(), nullIfEmpty(channelID), nullIfEmpty(incidentID), eventType, success, statusCode, errText)
	return translateError(err)
}

func scanNotificationChannel(row pgx.Row) (models.NotificationChannel, error) {
	var c models.NotificationChannel
	var configJSON []byte
	err := row.Scan(&c.ID, &c.OrganizationID, &c.Name, &c.Type, &c.URL, &configJSON, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if len(configJSON) > 0 {
		_ = json.Unmarshal(configJSON, &c.Config)
	}
	return c, nil
}

func marshalConfig(cfg map[string]any) ([]byte, error) {
	if cfg == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(cfg)
}
