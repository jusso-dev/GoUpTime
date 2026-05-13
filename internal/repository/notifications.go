package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

const notificationChannelColumns = `id, name, type, url, enabled, created_at, updated_at`

func (s *PostgresStore) ListNotificationChannels(ctx context.Context) ([]models.NotificationChannel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+notificationChannelColumns+`
		FROM notification_channels ORDER BY created_at DESC`)
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
	row := s.pool.QueryRow(ctx, `SELECT `+notificationChannelColumns+`
		FROM notification_channels WHERE id=$1`, id)
	c, err := scanNotificationChannel(row)
	return c, translateError(err)
}

func (s *PostgresStore) CreateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error) {
	if channel.ID == "" {
		channel.ID = uuid.NewString()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO notification_channels (id, name, type, url, enabled)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING `+notificationChannelColumns,
		channel.ID, channel.Name, channel.Type, channel.URL, channel.Enabled)
	c, err := scanNotificationChannel(row)
	return c, translateError(err)
}

func (s *PostgresStore) UpdateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error) {
	if channel.ID == "" {
		return models.NotificationChannel{}, fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE notification_channels SET name=$2, type=$3, url=$4, enabled=$5, updated_at=now()
		WHERE id=$1
		RETURNING `+notificationChannelColumns,
		channel.ID, channel.Name, channel.Type, channel.URL, channel.Enabled)
	c, err := scanNotificationChannel(row)
	return c, translateError(err)
}

func (s *PostgresStore) DeleteNotificationChannel(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM notification_channels WHERE id=$1`, id)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) LogNotificationEvent(ctx context.Context, channelID, incidentID, eventType string, success bool, statusCode int, errText string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO notification_events (id, channel_id, incident_id, event_type, success, status_code, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		uuid.NewString(), channelID, incidentID, eventType, success, statusCode, errText)
	return translateError(err)
}

func scanNotificationChannel(row pgx.Row) (models.NotificationChannel, error) {
	var c models.NotificationChannel
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.URL, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}
