package repository

import (
	"context"

	"github.com/jusso-dev/uptime/internal/models"
)

type Store interface {
	Ping(ctx context.Context) error
	CreateMonitor(ctx context.Context, monitor models.Monitor) (models.Monitor, error)
	ListMonitors(ctx context.Context) ([]models.Monitor, error)
	ListEnabledMonitors(ctx context.Context) ([]models.Monitor, error)
	GetMonitor(ctx context.Context, id string) (models.Monitor, error)
	UpdateMonitor(ctx context.Context, monitor models.Monitor) (models.Monitor, error)
	DeleteMonitor(ctx context.Context, id string) error
	UpdateMonitorStatus(ctx context.Context, id string, status models.CheckStatus) error

	CreateCheckResult(ctx context.Context, result models.CheckResult) (models.CheckResult, error)
	ListCheckResults(ctx context.Context, filter models.ResultFilter) ([]models.CheckResult, error)
	CountConsecutiveFailures(ctx context.Context, monitorID string) (int, error)

	ListIncidents(ctx context.Context) ([]models.Incident, error)
	GetIncident(ctx context.Context, id string) (models.Incident, error)
	GetOpenIncident(ctx context.Context, monitorID string) (*models.Incident, error)
	OpenIncident(ctx context.Context, incident models.Incident) (models.Incident, error)
	ResolveIncident(ctx context.Context, id string) (models.Incident, error)

	ListNotificationChannels(ctx context.Context) ([]models.NotificationChannel, error)
	CreateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error)
	UpdateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error)
	DeleteNotificationChannel(ctx context.Context, id string) error
	LogNotificationEvent(ctx context.Context, channelID, incidentID, eventType string, success bool, statusCode int, errText string) error

	CreateAPIKey(ctx context.Context, key models.APIKey) (models.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
	FindAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error)
	TouchAPIKey(ctx context.Context, id string) error
	RevokeAPIKey(ctx context.Context, id string) error

	OverviewStats(ctx context.Context) (models.OverviewStats, error)
}
