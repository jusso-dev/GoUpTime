package repository

import (
	"context"
	"time"

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
}
