package repository

import (
	"context"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

// NoopStore is a test helper that satisfies Store. Tests can embed it and
// override only the persistence methods relevant to the behavior under test.
type NoopStore struct{}

func (NoopStore) Ping(context.Context) error { return nil }
func (NoopStore) CreateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (NoopStore) ListMonitors(context.Context) ([]models.Monitor, error) { return nil, nil }
func (NoopStore) ListMonitorsFiltered(context.Context, models.MonitorFilter) ([]models.Monitor, error) {
	return nil, nil
}
func (NoopStore) ListEnabledMonitors(context.Context) ([]models.Monitor, error) { return nil, nil }
func (NoopStore) GetMonitor(context.Context, string) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (NoopStore) GetMonitorsByIDs(context.Context, string, []string) ([]models.Monitor, error) {
	return nil, nil
}
func (NoopStore) ListMonitorsByTags(context.Context, []string) ([]models.Monitor, error) {
	return nil, nil
}
func (NoopStore) SetMonitorTags(context.Context, string, []string) error { return nil }
func (NoopStore) UpdateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (NoopStore) DeleteMonitor(context.Context, string) error { return nil }
func (NoopStore) UpdateMonitorStatus(context.Context, string, models.CheckStatus) error {
	return nil
}
func (NoopStore) CreateCheckResult(context.Context, models.CheckResult) (models.CheckResult, error) {
	return models.CheckResult{}, nil
}
func (NoopStore) ListCheckResults(context.Context, models.ResultFilter) ([]models.CheckResult, error) {
	return nil, nil
}
func (NoopStore) CountConsecutiveFailures(context.Context, string) (int, error) { return 0, nil }
func (NoopStore) ListIncidents(context.Context) ([]models.Incident, error)      { return nil, nil }
func (NoopStore) GetIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (NoopStore) GetOpenIncident(context.Context, string) (*models.Incident, error) {
	return nil, nil
}
func (NoopStore) OpenIncident(context.Context, models.Incident) (models.Incident, error) {
	return models.Incident{}, nil
}
func (NoopStore) ResolveIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (NoopStore) AcknowledgeIncident(context.Context, string, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (NoopStore) ListNotificationChannels(context.Context) ([]models.NotificationChannel, error) {
	return nil, nil
}
func (NoopStore) GetNotificationChannel(context.Context, string) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (NoopStore) CreateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (NoopStore) UpdateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (NoopStore) DeleteNotificationChannel(context.Context, string) error { return nil }
func (NoopStore) LogNotificationEvent(context.Context, string, string, string, bool, int, string) error {
	return nil
}
func (NoopStore) CreateAPIKey(context.Context, models.APIKey) (models.APIKey, error) {
	return models.APIKey{}, nil
}
func (NoopStore) ListAPIKeys(context.Context) ([]models.APIKey, error) { return nil, nil }
func (NoopStore) FindAPIKeyByHash(context.Context, string) (*models.APIKey, error) {
	return nil, nil
}
func (NoopStore) TouchAPIKey(context.Context, string) error  { return nil }
func (NoopStore) RevokeAPIKey(context.Context, string) error { return nil }
func (NoopStore) OverviewStats(context.Context) (models.OverviewStats, error) {
	return models.OverviewStats{}, nil
}
func (NoopStore) UptimeReport(context.Context, models.UptimeReportFilter) (models.UptimeReport, error) {
	return models.UptimeReport{}, nil
}
func (NoopStore) SLAReportForMonitor(context.Context, string, time.Time, time.Time) (models.SLAReport, error) {
	return models.SLAReport{}, nil
}
func (NoopStore) SLAReportForOrg(context.Context, time.Time, time.Time) (models.SLAReport, error) {
	return models.SLAReport{}, nil
}
func (NoopStore) ExportCheckResults(context.Context, models.ResultFilter) ([]models.CheckResult, error) {
	return nil, nil
}
func (NoopStore) ExportIncidents(context.Context, models.ResultFilter) ([]models.Incident, error) {
	return nil, nil
}
func (NoopStore) ListServices(context.Context) ([]models.Service, error) { return nil, nil }
func (NoopStore) GetService(context.Context, string) (models.Service, error) {
	return models.Service{}, nil
}
func (NoopStore) CreateService(context.Context, models.Service) (models.Service, error) {
	return models.Service{}, nil
}
func (NoopStore) UpdateService(context.Context, models.Service) (models.Service, error) {
	return models.Service{}, nil
}
func (NoopStore) DeleteService(context.Context, string) error    { return nil }
func (NoopStore) ListTags(context.Context) ([]models.Tag, error) { return nil, nil }
func (NoopStore) CreateTag(context.Context, models.Tag) (models.Tag, error) {
	return models.Tag{}, nil
}
func (NoopStore) DeleteTag(context.Context, string) error { return nil }
func (NoopStore) ListMaintenanceWindows(context.Context) ([]models.MaintenanceWindow, error) {
	return nil, nil
}
func (NoopStore) GetMaintenanceWindow(context.Context, string) (models.MaintenanceWindow, error) {
	return models.MaintenanceWindow{}, nil
}
func (NoopStore) CreateMaintenanceWindow(context.Context, models.MaintenanceWindow) (models.MaintenanceWindow, error) {
	return models.MaintenanceWindow{}, nil
}
func (NoopStore) UpdateMaintenanceWindow(context.Context, models.MaintenanceWindow) (models.MaintenanceWindow, error) {
	return models.MaintenanceWindow{}, nil
}
func (NoopStore) DeleteMaintenanceWindow(context.Context, string) error { return nil }
func (NoopStore) ActiveMaintenanceForMonitor(context.Context, string, time.Time) (*models.MaintenanceWindow, error) {
	return nil, nil
}
func (NoopStore) IsMonitorInMaintenance(context.Context, string, time.Time) (bool, error) {
	return false, nil
}
func (NoopStore) ListStatusPages(context.Context) ([]models.StatusPage, error) { return nil, nil }
func (NoopStore) GetStatusPage(context.Context, string) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (NoopStore) GetStatusPageBySlug(context.Context, string) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (NoopStore) GetStatusPageByDomain(context.Context, string) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (NoopStore) CreateStatusPage(context.Context, models.StatusPage) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (NoopStore) UpdateStatusPage(context.Context, models.StatusPage) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (NoopStore) DeleteStatusPage(context.Context, string) error { return nil }
func (NoopStore) ListStatusPageComponents(context.Context, string) ([]models.StatusPageComponent, error) {
	return nil, nil
}
func (NoopStore) CreateStatusPageComponent(context.Context, models.StatusPageComponent) (models.StatusPageComponent, error) {
	return models.StatusPageComponent{}, nil
}
func (NoopStore) UpdateStatusPageComponent(context.Context, models.StatusPageComponent) (models.StatusPageComponent, error) {
	return models.StatusPageComponent{}, nil
}
func (NoopStore) UpsertStatusPageComponent(context.Context, models.StatusPageComponent) (models.StatusPageComponent, error) {
	return models.StatusPageComponent{}, nil
}
func (NoopStore) DeleteStatusPageComponent(context.Context, string) error { return nil }
func (NoopStore) PublicStatusPage(context.Context, string) (models.PublicStatusPage, error) {
	return models.PublicStatusPage{}, nil
}
func (NoopStore) GetOrganization(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (NoopStore) GetOrganizationByClerkID(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (NoopStore) UpsertOrganization(context.Context, models.Organization) (models.Organization, error) {
	return models.Organization{}, nil
}
func (NoopStore) DeleteOrganizationByClerkID(context.Context, string) error { return nil }
func (NoopStore) GetUserByID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (NoopStore) GetUserByClerkID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (NoopStore) UpsertUser(context.Context, models.User) (models.User, error) {
	return models.User{}, nil
}
func (NoopStore) DeleteUserByClerkID(context.Context, string) error { return nil }
func (NoopStore) ListMembershipsForUser(context.Context, string) ([]models.MembershipDetail, error) {
	return nil, nil
}
func (NoopStore) UpsertMembership(context.Context, models.Membership) error { return nil }
func (NoopStore) DeleteMembership(context.Context, string, string) error    { return nil }
func (NoopStore) RecordWebhookEvent(context.Context, string, string, []byte) (bool, error) {
	return true, nil
}
func (NoopStore) GetHeartbeat(context.Context, string) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (NoopStore) SetHeartbeat(context.Context, models.Heartbeat) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (NoopStore) DeleteHeartbeat(context.Context, string) error { return nil }
func (NoopStore) RecordHeartbeatPing(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (NoopStore) FindHeartbeatMonitorByTokenHash(context.Context, string) (*models.Monitor, error) {
	return nil, nil
}
func (NoopStore) RecordHeartbeat(context.Context, models.HeartbeatEvent) (models.HeartbeatEvent, error) {
	return models.HeartbeatEvent{}, nil
}
func (NoopStore) LastHeartbeat(context.Context, string) (*models.HeartbeatEvent, error) {
	return nil, nil
}
func (NoopStore) GetMultistepScript(context.Context, string) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (NoopStore) SetMultistepScript(context.Context, models.MultistepScript) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (NoopStore) GetBrowserScript(context.Context, string) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}
func (NoopStore) SetBrowserScript(context.Context, models.BrowserScript) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}
func (NoopStore) EnqueueNotification(context.Context, models.OutboxEntry) (models.OutboxEntry, error) {
	return models.OutboxEntry{}, nil
}
func (NoopStore) ClaimPendingNotifications(context.Context, int) ([]models.OutboxEntry, error) {
	return nil, nil
}
func (NoopStore) MarkNotificationDelivered(context.Context, string) error { return nil }
func (NoopStore) MarkNotificationRetry(context.Context, string, int, int, string, time.Time) error {
	return nil
}
func (NoopStore) UpsertPushDevice(context.Context, models.PushDevice) (models.PushDevice, error) {
	return models.PushDevice{}, nil
}
func (NoopStore) DeletePushDevice(context.Context, string) error { return nil }
func (NoopStore) ListPushDevicesForOrg(context.Context, string) ([]models.PushDevice, error) {
	return nil, nil
}
func (NoopStore) ListPushDevicesForUser(context.Context, string) ([]models.PushDevice, error) {
	return nil, nil
}
func (NoopStore) UpsertWorkerHeartbeat(context.Context, models.WorkerHeartbeat) error { return nil }
func (NoopStore) ListWorkerHeartbeats(context.Context, time.Time) ([]models.WorkerHeartbeat, error) {
	return nil, nil
}
func (NoopStore) DeleteWorkerHeartbeat(context.Context, string) error { return nil }
