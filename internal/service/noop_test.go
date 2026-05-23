package service

import (
	"context"
	"time"

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
func (StoreNoop) GetNotificationChannel(context.Context, string) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
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
func (StoreNoop) UpsertWorkerHeartbeat(context.Context, models.WorkerHeartbeat) error { return nil }
func (StoreNoop) ListWorkerHeartbeats(context.Context, time.Time) ([]models.WorkerHeartbeat, error) {
	return nil, nil
}
func (StoreNoop) DeleteWorkerHeartbeat(context.Context, string) error { return nil }

func (StoreNoop) AcknowledgeIncident(context.Context, string, string) (models.Incident, error) {
	return models.Incident{}, nil
}

func (StoreNoop) GetOrganization(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (StoreNoop) GetOrganizationByClerkID(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (StoreNoop) UpsertOrganization(context.Context, models.Organization) (models.Organization, error) {
	return models.Organization{}, nil
}
func (StoreNoop) DeleteOrganizationByClerkID(context.Context, string) error { return nil }

func (StoreNoop) GetUserByID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (StoreNoop) GetUserByClerkID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (StoreNoop) UpsertUser(context.Context, models.User) (models.User, error) {
	return models.User{}, nil
}
func (StoreNoop) DeleteUserByClerkID(context.Context, string) error { return nil }

func (StoreNoop) ListMembershipsForUser(context.Context, string) ([]models.MembershipDetail, error) {
	return nil, nil
}
func (StoreNoop) UpsertMembership(context.Context, models.Membership) error { return nil }
func (StoreNoop) DeleteMembership(context.Context, string, string) error    { return nil }

func (StoreNoop) RecordWebhookEvent(context.Context, string, string, []byte) (bool, error) {
	return true, nil
}

func (StoreNoop) GetHeartbeat(context.Context, string) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (StoreNoop) SetHeartbeat(context.Context, models.Heartbeat) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (StoreNoop) DeleteHeartbeat(context.Context, string) error { return nil }
func (StoreNoop) RecordHeartbeatPing(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (StoreNoop) GetMultistepScript(context.Context, string) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (StoreNoop) SetMultistepScript(context.Context, models.MultistepScript) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (StoreNoop) GetBrowserScript(context.Context, string) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}
func (StoreNoop) SetBrowserScript(context.Context, models.BrowserScript) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}
