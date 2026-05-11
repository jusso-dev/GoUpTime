package service

import (
	"context"

	"github.com/jusso-dev/uptime/internal/models"
)

type StoreNoop struct{}

func (StoreNoop) Ping(context.Context) error { return nil }
func (StoreNoop) CreateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (StoreNoop) ListMonitors(context.Context) ([]models.Monitor, error)        { return nil, nil }
func (StoreNoop) ListEnabledMonitors(context.Context) ([]models.Monitor, error) { return nil, nil }
func (StoreNoop) GetMonitor(context.Context, string) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (StoreNoop) UpdateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (StoreNoop) DeleteMonitor(context.Context, string) error                           { return nil }
func (StoreNoop) UpdateMonitorStatus(context.Context, string, models.CheckStatus) error { return nil }
func (StoreNoop) CreateCheckResult(context.Context, models.CheckResult) (models.CheckResult, error) {
	return models.CheckResult{}, nil
}
func (StoreNoop) ListCheckResults(context.Context, models.ResultFilter) ([]models.CheckResult, error) {
	return nil, nil
}
func (StoreNoop) CountConsecutiveFailures(context.Context, string) (int, error) { return 0, nil }
func (StoreNoop) ListIncidents(context.Context) ([]models.Incident, error)      { return nil, nil }
func (StoreNoop) GetIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (StoreNoop) GetOpenIncident(context.Context, string) (*models.Incident, error) { return nil, nil }
func (StoreNoop) OpenIncident(context.Context, models.Incident) (models.Incident, error) {
	return models.Incident{}, nil
}
func (StoreNoop) ResolveIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (StoreNoop) ListNotificationChannels(context.Context) ([]models.NotificationChannel, error) {
	return nil, nil
}
func (StoreNoop) CreateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (StoreNoop) UpdateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (StoreNoop) DeleteNotificationChannel(context.Context, string) error { return nil }
func (StoreNoop) LogNotificationEvent(context.Context, string, string, string, bool, int, string) error {
	return nil
}
func (StoreNoop) CreateAPIKey(context.Context, models.APIKey) (models.APIKey, error) {
	return models.APIKey{}, nil
}
func (StoreNoop) ListAPIKeys(context.Context) ([]models.APIKey, error)             { return nil, nil }
func (StoreNoop) FindAPIKeyByHash(context.Context, string) (*models.APIKey, error) { return nil, nil }
func (StoreNoop) TouchAPIKey(context.Context, string) error                        { return nil }
func (StoreNoop) RevokeAPIKey(context.Context, string) error                       { return nil }
func (StoreNoop) OverviewStats(context.Context) (models.OverviewStats, error) {
	return models.OverviewStats{}, nil
}
