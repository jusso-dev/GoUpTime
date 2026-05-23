package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/models"
)

// Store is the persistence boundary used by services and HTTP handlers. The
// interface keeps SQL out of the rest of the codebase and gives tests a
// natural seam for fakes (see internal/service/StoreNoop).
//
// All tenant-scoped methods read the org context from ctx via
// auth.FromContext. They return apierr.ErrNotFound when a row exists but
// belongs to a different organization — repository methods must never leak
// existence across tenants.
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
	AcknowledgeIncident(ctx context.Context, id, userID string) (models.Incident, error)

	ListNotificationChannels(ctx context.Context) ([]models.NotificationChannel, error)
	GetNotificationChannel(ctx context.Context, id string) (models.NotificationChannel, error)
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

	UpsertWorkerHeartbeat(ctx context.Context, hb models.WorkerHeartbeat) error
	ListWorkerHeartbeats(ctx context.Context, since time.Time) ([]models.WorkerHeartbeat, error)
	DeleteWorkerHeartbeat(ctx context.Context, instanceID string) error

	// Multi-tenancy.
	GetOrganization(ctx context.Context, id string) (models.Organization, error)
	GetOrganizationByClerkID(ctx context.Context, clerkOrgID string) (models.Organization, error)
	UpsertOrganization(ctx context.Context, org models.Organization) (models.Organization, error)
	DeleteOrganizationByClerkID(ctx context.Context, clerkOrgID string) error

	GetUserByID(ctx context.Context, id string) (models.User, error)
	GetUserByClerkID(ctx context.Context, clerkUserID string) (models.User, error)
	UpsertUser(ctx context.Context, user models.User) (models.User, error)
	DeleteUserByClerkID(ctx context.Context, clerkUserID string) error

	ListMembershipsForUser(ctx context.Context, userID string) ([]models.MembershipDetail, error)
	UpsertMembership(ctx context.Context, m models.Membership) error
	DeleteMembership(ctx context.Context, organizationID, userID string) error

	// Webhook event dedup: returns true if the event id was newly recorded,
	// false if it had been seen before.
	RecordWebhookEvent(ctx context.Context, id, source string, payload []byte) (bool, error)

	// Heartbeat (push monitor) management.
	GetHeartbeat(ctx context.Context, monitorID string) (models.Heartbeat, error)
	SetHeartbeat(ctx context.Context, hb models.Heartbeat) (models.Heartbeat, error)
	DeleteHeartbeat(ctx context.Context, monitorID string) error
	RecordHeartbeatPing(ctx context.Context, tokenHash, sourceIP, userAgent string) (string, error)

	// Per-monitor script storage for the multistep and browser check types.
	GetMultistepScript(ctx context.Context, monitorID string) (models.MultistepScript, error)
	SetMultistepScript(ctx context.Context, script models.MultistepScript) (models.MultistepScript, error)
	GetBrowserScript(ctx context.Context, monitorID string) (models.BrowserScript, error)
	SetBrowserScript(ctx context.Context, script models.BrowserScript) (models.BrowserScript, error)

	// Notification outbox: durable retry queue for incident dispatches.
	EnqueueNotification(ctx context.Context, entry models.OutboxEntry) (models.OutboxEntry, error)
	ClaimPendingNotifications(ctx context.Context, limit int) ([]models.OutboxEntry, pgx.Tx, error)
	MarkNotificationDelivered(ctx context.Context, tx pgx.Tx, id string) error
	MarkNotificationRetry(ctx context.Context, tx pgx.Tx, id string, attempts, maxAttempts int, lastErr string, next time.Time) error

	// Mobile push device registry.
	UpsertPushDevice(ctx context.Context, device models.PushDevice) (models.PushDevice, error)
	DeletePushDevice(ctx context.Context, id string) error
	ListPushDevicesForOrg(ctx context.Context, organizationID string) ([]models.PushDevice, error)
	ListPushDevicesForUser(ctx context.Context, userID string) ([]models.PushDevice, error)

	// Public status pages.
	ListStatusPages(ctx context.Context) ([]models.StatusPage, error)
	CreateStatusPage(ctx context.Context, page models.StatusPage) (models.StatusPage, error)
	GetStatusPageBySlug(ctx context.Context, slug string) (models.StatusPage, error)
	GetStatusPageByDomain(ctx context.Context, domain string) (models.StatusPage, error)
	DeleteStatusPage(ctx context.Context, id string) error
	ListStatusPageComponents(ctx context.Context, pageID string) ([]models.StatusPageComponent, error)
	UpsertStatusPageComponent(ctx context.Context, c models.StatusPageComponent) (models.StatusPageComponent, error)
	DeleteStatusPageComponent(ctx context.Context, id string) error
	GetMonitorsByIDs(ctx context.Context, organizationID string, ids []string) ([]models.Monitor, error)

	// Maintenance windows.
	ListMaintenanceWindows(ctx context.Context) ([]models.MaintenanceWindow, error)
	CreateMaintenanceWindow(ctx context.Context, w models.MaintenanceWindow) (models.MaintenanceWindow, error)
	DeleteMaintenanceWindow(ctx context.Context, id string) error
	IsMonitorInMaintenance(ctx context.Context, monitorID string, at time.Time) (bool, error)

	// Tags.
	ListTags(ctx context.Context) ([]models.Tag, error)
	CreateTag(ctx context.Context, t models.Tag) (models.Tag, error)
	DeleteTag(ctx context.Context, id string) error
	SetMonitorTags(ctx context.Context, monitorID string, tagIDs []string) error
	ListMonitorsByTags(ctx context.Context, names []string) ([]models.Monitor, error)

	// SLA reports.
	SLAReportForMonitor(ctx context.Context, monitorID string, from, to time.Time) (models.SLAReport, error)
	SLAReportForOrg(ctx context.Context, from, to time.Time) (models.SLAReport, error)
}
