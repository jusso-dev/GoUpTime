package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

// PostgresStore is the GORM-backed persistence implementation. The rest of
// the app depends on repository.Store, so ORM details stay in this package.
type PostgresStore struct {
	db *gorm.DB
}

// PoolConfig tunes the database/sql pool under GORM. Zero values fall back to
// conservative defaults suitable for a single small deployment.
type PoolConfig struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConns:        20,
		MinConns:        2,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
	}
}

func Open(ctx context.Context, databaseURL string, pc PoolConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap database handle: %w", err)
	}
	if pc.MaxConns > 0 {
		sqlDB.SetMaxOpenConns(int(pc.MaxConns))
	}
	if pc.MinConns > 0 {
		sqlDB.SetMaxIdleConns(int(pc.MinConns))
	}
	if pc.MaxConnLifetime > 0 {
		sqlDB.SetConnMaxLifetime(pc.MaxConnLifetime)
	}
	if pc.MaxConnIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(pc.MaxConnIdleTime)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func AutoMigrate(ctx context.Context, db *gorm.DB) error {
	return translateError(db.WithContext(ctx).AutoMigrate(
		&dbOrganization{},
		&dbUser{},
		&dbMembership{},
		&dbWebhookEvent{},
		&dbService{},
		&dbTag{},
		&dbMonitor{},
		&dbMonitorTag{},
		&dbCheckResult{},
		&dbIncident{},
		&dbIncidentTimelineEvent{},
		&dbIncidentComment{},
		&dbIncidentPostmortem{},
		&dbIncidentActionItem{},
		&dbIncidentSuppression{},
		&dbMonitorDependency{},
		&dbNotificationChannel{},
		&dbNotificationEvent{},
		&dbOutboxEntry{},
		&dbPushDevice{},
		&dbAgent{},
		&dbAPIKey{},
		&dbAuditLog{},
		&dbWorkerHeartbeat{},
		&dbMaintenanceWindow{},
		&dbMaintenanceWindowMonitor{},
		&dbMaintenanceWindowTag{},
		&dbStatusPage{},
		&dbStatusPageComponent{},
		&dbStatusPageComponentMonitor{},
		&dbHeartbeat{},
		&dbMultistepScript{},
		&dbBrowserScript{},
		&dbStatusPageSubscriber{},
		&dbStatusPageSubscriberComponent{},
		&dbStatusPageAnnouncement{},
		&dbStatusPageAnnouncementComponent{},
		&dbOnCallSchedule{},
		&dbOnCallOverride{},
		&dbEscalationPolicy{},
		&dbRunbook{},
		&dbBrowserArtifact{},
		&dbHeartbeatEvent{},
	))
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apierr.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return apierr.ErrConflict
	}
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return apierr.ErrInvalidInput
	}
	return err
}

type dbService struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index;uniqueIndex:idx_services_org_slug"`
	Name           string
	Slug           string `gorm:"uniqueIndex:idx_services_org_slug"`
	Description    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbService) TableName() string { return "services" }

type dbTag struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index;uniqueIndex:idx_tags_org_name"`
	Name           string `gorm:"uniqueIndex:idx_tags_org_name"`
	Color          string
	CreatedAt      time.Time
}

func (dbTag) TableName() string { return "tags" }

type dbMonitor struct {
	ID                          string `gorm:"type:uuid;primaryKey"`
	OrganizationID              string `gorm:"type:uuid;index"`
	Name                        string
	Type                        string
	Target                      string
	Method                      string
	ExpectedStatus              int
	ExpectedKeyword             string
	TimeoutSeconds              int
	IntervalSeconds             int
	FailureThreshold            int
	Enabled                     bool
	Status                      string
	ServiceID                   *string                     `gorm:"type:uuid;index"`
	Config                      datatypes.JSONMap           `gorm:"type:jsonb"`
	Tags                        []dbTag                     `gorm:"many2many:monitor_tags;joinForeignKey:MonitorID;joinReferences:TagID;constraint:OnDelete:CASCADE;"`
	Regions                     datatypes.JSONSlice[string] `gorm:"type:jsonb"`
	RegionConfirmationThreshold int
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (dbMonitor) TableName() string { return "monitors" }

type dbCheckResult struct {
	ID                    string `gorm:"type:uuid;primaryKey"`
	OrganizationID        string `gorm:"type:uuid;index"`
	MonitorID             string `gorm:"type:uuid;index"`
	Status                string
	Success               bool
	ResponseTimeMS        int64
	StatusCode            int
	Error                 string
	CheckedAt             time.Time `gorm:"index"`
	DNSMS                 int64
	TCPConnectMS          int64
	TLSHandshakeMS        int64
	TimeToFirstByteMS     int64
	TotalMS               int64
	ResponseSnippet       string
	MaintenanceSuppressed bool
	Metadata              datatypes.JSONMap `gorm:"type:jsonb"`
	DomainExpiresAt       *time.Time
	Region                string `gorm:"index"`
}

func (dbCheckResult) TableName() string { return "check_results" }

type dbIncident struct {
	ID                   string `gorm:"type:uuid;primaryKey"`
	OrganizationID       string `gorm:"type:uuid;index"`
	MonitorID            string `gorm:"type:uuid;index"`
	Status               string `gorm:"index"`
	Severity             string
	Impact               string
	StartedAt            time.Time
	ResolvedAt           *time.Time
	AcknowledgedAt       *time.Time
	AcknowledgedByUserID string
	AssignedToUserID     string
	ResolvedByUserID     string
	GroupKey             string `gorm:"index"`
	ErrorClass           string
	Flapping             bool
	Suppressed           bool
	SuppressionReason    string
	Reason               string
	LastError            string
	ConsecutiveFailures  int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (dbIncident) TableName() string { return "incidents" }

type dbNotificationChannel struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	Name           string
	Type           string
	URL            string
	Config         datatypes.JSONMap `gorm:"type:jsonb"`
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbNotificationChannel) TableName() string { return "notification_channels" }

type dbNotificationEvent struct {
	ID         string  `gorm:"type:uuid;primaryKey"`
	ChannelID  *string `gorm:"type:uuid;index"`
	IncidentID *string `gorm:"type:uuid;index"`
	EventType  string
	Success    bool
	StatusCode int
	Error      string
	CreatedAt  time.Time
}

func (dbNotificationEvent) TableName() string { return "notification_events" }

type dbAPIKey struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	Name           string
	KeyHash        string `gorm:"uniqueIndex"`
	CreatedAt      time.Time
	LastUsedAt     *time.Time
	RevokedAt      *time.Time `gorm:"index"`
}

func (dbAPIKey) TableName() string { return "api_keys" }

type dbAuditLog struct {
	ID         string `gorm:"type:uuid;primaryKey"`
	Actor      string
	Action     string
	TargetType string
	TargetID   string
	Metadata   datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt  time.Time
}

func (dbAuditLog) TableName() string { return "audit_logs" }

type dbWorkerHeartbeat struct {
	InstanceID    string `gorm:"primaryKey"`
	Hostname      string
	Version       string
	Region        string
	StartedAt     time.Time
	LastSeenAt    time.Time `gorm:"index"`
	WorkerCount   int
	ActiveJobs    int
	QueueDepth    int
	QueueCapacity int
	JobsCompleted int64
	JobsFailed    int64
	InFlight      datatypes.JSONSlice[string] `gorm:"type:jsonb"`
}

func (dbWorkerHeartbeat) TableName() string { return "worker_heartbeats" }

type dbMaintenanceWindow struct {
	ID              string `gorm:"type:uuid;primaryKey"`
	OrganizationID  string `gorm:"type:uuid;index"`
	Name            string
	Description     string
	StartsAt        time.Time
	EndsAt          time.Time
	Timezone        string
	Recurrence      string
	RecurrenceRRule string
	StatusPageID    *string `gorm:"type:uuid;index"`
	CreatedByUserID string  `gorm:"type:uuid"`
	Enabled         bool
	Monitors        []dbMonitor `gorm:"many2many:maintenance_window_monitors;joinForeignKey:MaintenanceWindowID;joinReferences:MonitorID;constraint:OnDelete:CASCADE;"`
	Tags            []dbTag     `gorm:"many2many:maintenance_window_tags;joinForeignKey:MaintenanceWindowID;joinReferences:TagID;constraint:OnDelete:CASCADE;"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (dbMaintenanceWindow) TableName() string { return "maintenance_windows" }

type dbMaintenanceWindowMonitor struct {
	MaintenanceWindowID string `gorm:"type:uuid;primaryKey"`
	MonitorID           string `gorm:"type:uuid;primaryKey"`
}

func (dbMaintenanceWindowMonitor) TableName() string { return "maintenance_window_monitors" }

type dbMaintenanceWindowTag struct {
	MaintenanceWindowID string `gorm:"type:uuid;primaryKey"`
	TagID               string `gorm:"type:uuid;primaryKey"`
}

func (dbMaintenanceWindowTag) TableName() string { return "maintenance_window_tags" }

type dbStatusPage struct {
	ID                   string `gorm:"type:uuid;primaryKey"`
	OrganizationID       string `gorm:"type:uuid;index"`
	Slug                 string `gorm:"uniqueIndex"`
	Name                 string
	Description          string
	CustomDomain         string `gorm:"uniqueIndex"`
	CustomDomainVerified bool
	Theme                datatypes.JSONMap `gorm:"type:jsonb"`
	Published            bool
	AutoUpdates          bool
	LogoURL              string
	PrimaryColor         string
	Public               bool
	NoIndex              bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (dbStatusPage) TableName() string { return "status_pages" }

type dbStatusPageComponent struct {
	ID           string `gorm:"type:uuid;primaryKey"`
	StatusPageID string `gorm:"type:uuid;index"`
	Name         string
	Description  string
	Position     int
	GroupName    string
	ServiceID    *string `gorm:"type:uuid;index"`
	ManualStatus string
	OrderIndex   int
	Monitors     []dbMonitor `gorm:"many2many:status_page_component_monitors;joinForeignKey:ComponentID;joinReferences:MonitorID;constraint:OnDelete:CASCADE;"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (dbStatusPageComponent) TableName() string { return "status_page_components" }

type dbStatusPageComponentMonitor struct {
	ComponentID string `gorm:"type:uuid;primaryKey"`
	MonitorID   string `gorm:"type:uuid;primaryKey"`
}

func (dbStatusPageComponentMonitor) TableName() string { return "status_page_component_monitors" }

type dbOrganization struct {
	ID         string `gorm:"type:uuid;primaryKey"`
	ClerkOrgID string `gorm:"uniqueIndex"`
	Name       string
	Slug       string
	Plan       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (dbOrganization) TableName() string { return "organizations" }

type dbUser struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	ClerkUserID string `gorm:"uniqueIndex"`
	Email       string
	Name        string
	ImageURL    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (dbUser) TableName() string { return "users" }

type dbMembership struct {
	OrganizationID string `gorm:"type:uuid;primaryKey"`
	UserID         string `gorm:"type:uuid;primaryKey"`
	Role           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbMembership) TableName() string { return "memberships" }

type dbWebhookEvent struct {
	ID        string `gorm:"primaryKey"`
	Source    string
	Payload   datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt time.Time
}

func (dbWebhookEvent) TableName() string { return "webhook_events" }

type dbMonitorTag struct {
	MonitorID string `gorm:"type:uuid;primaryKey"`
	TagID     string `gorm:"type:uuid;primaryKey"`
}

func (dbMonitorTag) TableName() string { return "monitor_tags" }

type dbHeartbeat struct {
	MonitorID               string `gorm:"type:uuid;primaryKey"`
	TokenHash               string `gorm:"uniqueIndex"`
	ExpectedIntervalSeconds int
	GraceSeconds            int
	LastPingAt              *time.Time
	LastPingSourceIP        string
	LastPingUserAgent       string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (dbHeartbeat) TableName() string { return "heartbeats" }

type dbMultistepScript struct {
	MonitorID string            `gorm:"type:uuid;primaryKey"`
	Steps     datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (dbMultistepScript) TableName() string { return "multistep_scripts" }

type dbBrowserScript struct {
	MonitorID      string `gorm:"type:uuid;primaryKey"`
	Source         string
	TimeoutSeconds int
	Retries        int
	Env            datatypes.JSONMap `gorm:"type:jsonb"`
	RetentionDays  int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbBrowserScript) TableName() string { return "browser_scripts" }

type dbOutboxEntry struct {
	ID             string  `gorm:"type:uuid;primaryKey"`
	OrganizationID string  `gorm:"type:uuid;index"`
	ChannelID      *string `gorm:"type:uuid;index"`
	IncidentID     *string `gorm:"type:uuid;index"`
	EventType      string
	Payload        []byte
	Attempts       int
	NextAttemptAt  time.Time `gorm:"index"`
	Status         string    `gorm:"index"`
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbOutboxEntry) TableName() string { return "notification_outbox" }

type dbPushDevice struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	UserID         string `gorm:"type:uuid;index"`
	Platform       string
	ExpoToken      string `gorm:"uniqueIndex"`
	AppVersion     string
	LastSeenAt     time.Time
	CreatedAt      time.Time
}

func (dbPushDevice) TableName() string { return "push_devices" }

type dbHeartbeatEvent struct {
	ID         string `gorm:"type:uuid;primaryKey"`
	MonitorID  string `gorm:"type:uuid;index"`
	Status     string
	Message    string
	DurationMS int64
	Metadata   datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt  time.Time         `gorm:"index"`
}

func (dbHeartbeatEvent) TableName() string { return "heartbeat_events" }

func newID(value string) string {
	if value != "" {
		return value
	}
	return uuid.NewString()
}

func normalizeMonitor(m models.Monitor) models.Monitor {
	m.ID = newID(m.ID)
	if m.Method == "" {
		m.Method = "GET"
	}
	if m.ExpectedStatus == 0 && (m.Type == models.MonitorHTTP || m.Type == models.MonitorKeyword || m.Type == models.MonitorAPI) {
		m.ExpectedStatus = 200
	}
	if m.TimeoutSeconds == 0 {
		m.TimeoutSeconds = 10
	}
	if m.IntervalSeconds == 0 {
		m.IntervalSeconds = 60
	}
	if m.FailureThreshold == 0 {
		m.FailureThreshold = 3
	}
	if m.Status == "" {
		m.Status = models.StatusDegraded
	}
	if m.Config == nil {
		m.Config = map[string]any{}
	}
	if len(m.Regions) == 0 {
		m.Regions = []string{"default"}
	}
	if m.RegionConfirmationThreshold <= 0 {
		m.RegionConfirmationThreshold = 1
	}
	if m.RegionConfirmationThreshold > len(m.Regions) {
		m.RegionConfirmationThreshold = len(m.Regions)
	}
	m.Tags = normalizeTags(m.Tags)
	return m
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range tags {
		name := strings.ToLower(strings.TrimSpace(tag))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	v := value
	return &v
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func jsonMap(value map[string]any) datatypes.JSONMap {
	if value == nil {
		return datatypes.JSONMap{}
	}
	return datatypes.JSONMap(value)
}

func stringJSONMap(value map[string]string) datatypes.JSONMap {
	out := datatypes.JSONMap{}
	for k, v := range value {
		out[k] = v
	}
	return out
}

func modelJSON(value datatypes.JSONMap) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for k, v := range value {
		out[k] = v
	}
	return out
}

func modelStringJSON(value datatypes.JSONMap) map[string]string {
	out := map[string]string{}
	for k, v := range value {
		switch typed := v.(type) {
		case string:
			out[k] = typed
		case fmt.Stringer:
			out[k] = typed.String()
		case nil:
		default:
			out[k] = fmt.Sprint(typed)
		}
	}
	return out
}

func redactedJSON(value datatypes.JSONMap) map[string]any {
	out := modelJSON(value)
	for key := range out {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") ||
			strings.Contains(lower, "token") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "key") {
			out[key] = "********"
		}
	}
	return out
}

func (m dbMonitor) toModel() models.Monitor {
	tags := make([]string, 0, len(m.Tags))
	for _, tag := range m.Tags {
		tags = append(tags, tag.Name)
	}
	sort.Strings(tags)
	return models.Monitor{
		ID:                          m.ID,
		OrganizationID:              m.OrganizationID,
		Name:                        m.Name,
		Type:                        models.MonitorType(m.Type),
		Target:                      m.Target,
		Method:                      m.Method,
		ExpectedStatus:              m.ExpectedStatus,
		ExpectedKeyword:             m.ExpectedKeyword,
		TimeoutSeconds:              m.TimeoutSeconds,
		IntervalSeconds:             m.IntervalSeconds,
		FailureThreshold:            m.FailureThreshold,
		Enabled:                     m.Enabled,
		Status:                      models.CheckStatus(m.Status),
		ServiceID:                   stringValue(m.ServiceID),
		Tags:                        tags,
		Config:                      modelJSON(m.Config),
		Regions:                     []string(m.Regions),
		RegionConfirmationThreshold: m.RegionConfirmationThreshold,
		CreatedAt:                   m.CreatedAt,
		UpdatedAt:                   m.UpdatedAt,
	}
}

func monitorRow(m models.Monitor) dbMonitor {
	return dbMonitor{
		ID:                          m.ID,
		OrganizationID:              m.OrganizationID,
		Name:                        m.Name,
		Type:                        string(m.Type),
		Target:                      m.Target,
		Method:                      m.Method,
		ExpectedStatus:              m.ExpectedStatus,
		ExpectedKeyword:             m.ExpectedKeyword,
		TimeoutSeconds:              m.TimeoutSeconds,
		IntervalSeconds:             m.IntervalSeconds,
		FailureThreshold:            m.FailureThreshold,
		Enabled:                     m.Enabled,
		Status:                      string(m.Status),
		ServiceID:                   stringPtr(m.ServiceID),
		Config:                      jsonMap(m.Config),
		Regions:                     datatypes.NewJSONSlice(m.Regions),
		RegionConfirmationThreshold: m.RegionConfirmationThreshold,
	}
}

func (r dbCheckResult) toModel() models.CheckResult {
	return models.CheckResult{
		ID:                    r.ID,
		OrganizationID:        r.OrganizationID,
		MonitorID:             r.MonitorID,
		Status:                models.CheckStatus(r.Status),
		Success:               r.Success,
		ResponseTimeMS:        r.ResponseTimeMS,
		StatusCode:            r.StatusCode,
		Error:                 r.Error,
		CheckedAt:             r.CheckedAt,
		DNSMS:                 r.DNSMS,
		TCPConnectMS:          r.TCPConnectMS,
		TLSHandshakeMS:        r.TLSHandshakeMS,
		TimeToFirstByteMS:     r.TimeToFirstByteMS,
		TotalMS:               r.TotalMS,
		ResponseSnippet:       r.ResponseSnippet,
		MaintenanceSuppressed: r.MaintenanceSuppressed,
		Metadata:              modelJSON(r.Metadata),
		DomainExpiresAt:       r.DomainExpiresAt,
		Region:                r.Region,
	}
}

func checkResultRow(r models.CheckResult) dbCheckResult {
	return dbCheckResult{
		ID:                    newID(r.ID),
		OrganizationID:        r.OrganizationID,
		MonitorID:             r.MonitorID,
		Status:                string(r.Status),
		Success:               r.Success,
		ResponseTimeMS:        r.ResponseTimeMS,
		StatusCode:            r.StatusCode,
		Error:                 r.Error,
		CheckedAt:             r.CheckedAt,
		DNSMS:                 r.DNSMS,
		TCPConnectMS:          r.TCPConnectMS,
		TLSHandshakeMS:        r.TLSHandshakeMS,
		TimeToFirstByteMS:     r.TimeToFirstByteMS,
		TotalMS:               r.TotalMS,
		ResponseSnippet:       r.ResponseSnippet,
		MaintenanceSuppressed: r.MaintenanceSuppressed,
		Metadata:              jsonMap(r.Metadata),
		DomainExpiresAt:       r.DomainExpiresAt,
		Region:                r.Region,
	}
}

func (i dbIncident) toModel() models.Incident {
	return models.Incident{
		ID:                   i.ID,
		OrganizationID:       i.OrganizationID,
		MonitorID:            i.MonitorID,
		Status:               models.IncidentStatus(i.Status),
		Severity:             models.IncidentSeverity(i.Severity),
		Impact:               models.IncidentImpact(i.Impact),
		StartedAt:            i.StartedAt,
		ResolvedAt:           i.ResolvedAt,
		AcknowledgedAt:       i.AcknowledgedAt,
		AcknowledgedByUserID: i.AcknowledgedByUserID,
		AssignedToUserID:     i.AssignedToUserID,
		ResolvedByUserID:     i.ResolvedByUserID,
		GroupKey:             i.GroupKey,
		ErrorClass:           i.ErrorClass,
		Flapping:             i.Flapping,
		Suppressed:           i.Suppressed,
		SuppressionReason:    i.SuppressionReason,
		Reason:               i.Reason,
		LastError:            i.LastError,
		ConsecutiveFailures:  i.ConsecutiveFailures,
		CreatedAt:            i.CreatedAt,
		UpdatedAt:            i.UpdatedAt,
	}
}

func incidentRow(i models.Incident) dbIncident {
	return dbIncident{
		ID:                   newID(i.ID),
		OrganizationID:       i.OrganizationID,
		MonitorID:            i.MonitorID,
		Status:               string(i.Status),
		Severity:             string(i.Severity),
		Impact:               string(i.Impact),
		StartedAt:            i.StartedAt,
		ResolvedAt:           i.ResolvedAt,
		AcknowledgedAt:       i.AcknowledgedAt,
		AcknowledgedByUserID: i.AcknowledgedByUserID,
		AssignedToUserID:     i.AssignedToUserID,
		ResolvedByUserID:     i.ResolvedByUserID,
		GroupKey:             i.GroupKey,
		ErrorClass:           i.ErrorClass,
		Flapping:             i.Flapping,
		Suppressed:           i.Suppressed,
		SuppressionReason:    i.SuppressionReason,
		Reason:               i.Reason,
		LastError:            i.LastError,
		ConsecutiveFailures:  i.ConsecutiveFailures,
	}
}

func (c dbNotificationChannel) toModel() models.NotificationChannel {
	return models.NotificationChannel{
		ID:             c.ID,
		OrganizationID: c.OrganizationID,
		Name:           c.Name,
		Type:           c.Type,
		URL:            c.URL,
		Config:         redactedJSON(c.Config),
		Enabled:        c.Enabled,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
}

func notificationChannelRow(c models.NotificationChannel) dbNotificationChannel {
	return dbNotificationChannel{
		ID:             newID(c.ID),
		OrganizationID: c.OrganizationID,
		Name:           c.Name,
		Type:           c.Type,
		URL:            c.URL,
		Config:         jsonMap(c.Config),
		Enabled:        c.Enabled,
	}
}

func (s *PostgresStore) CreateMonitor(ctx context.Context, monitor models.Monitor) (models.Monitor, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Monitor{}, err
	}
	monitor = normalizeMonitor(monitor)
	monitor.OrganizationID = orgID
	row := monitorRow(monitor)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return replaceMonitorTags(ctx, tx, &row, monitor.Tags, orgID)
	})
	if err != nil {
		return models.Monitor{}, translateError(err)
	}
	return s.GetMonitor(ctx, row.ID)
}

func (s *PostgresStore) ListMonitors(ctx context.Context) ([]models.Monitor, error) {
	return s.ListMonitorsFiltered(ctx, models.MonitorFilter{})
}

func (s *PostgresStore) ListMonitorsFiltered(ctx context.Context, filter models.MonitorFilter) ([]models.Monitor, error) {
	var rows []dbMonitor
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Model(&dbMonitor{}).Preload("Tags").Order("created_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if filter.Tag != "" {
		q = q.Joins("JOIN monitor_tags ON monitor_tags.monitor_id = monitors.id").
			Joins("JOIN tags ON tags.id = monitor_tags.tag_id").
			Where("tags.name = ?", strings.ToLower(strings.TrimSpace(filter.Tag)))
		if !skip {
			q = q.Where("tags.organization_id = ?", orgID)
		}
	}
	if filter.ServiceID != "" {
		q = q.Where("service_id = ?", filter.ServiceID)
	}
	if filter.Type != "" {
		q = q.Where("type = ?", filter.Type)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.Enabled != nil {
		q = q.Where("enabled = ?", *filter.Enabled)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return monitorsFromRows(rows), nil
}

func (s *PostgresStore) ListEnabledMonitors(ctx context.Context) ([]models.Monitor, error) {
	enabled := true
	return s.ListMonitorsFiltered(ctx, models.MonitorFilter{Enabled: &enabled})
}

func (s *PostgresStore) GetMonitor(ctx context.Context, id string) (models.Monitor, error) {
	if id == "" {
		return models.Monitor{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Monitor{}, err
	}
	var row dbMonitor
	q := s.db.WithContext(ctx).Preload("Tags").Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return row.toModel(), translateError(err)
}

func (s *PostgresStore) UpdateMonitor(ctx context.Context, monitor models.Monitor) (models.Monitor, error) {
	if monitor.ID == "" {
		return models.Monitor{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Monitor{}, err
	}
	monitor = normalizeMonitor(monitor)
	monitor.OrganizationID = orgID
	row := monitorRow(monitor)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&dbMonitor{}).Where("id = ? AND organization_id = ?", monitor.ID, orgID).Updates(map[string]any{
			"name":              row.Name,
			"type":              row.Type,
			"target":            row.Target,
			"method":            row.Method,
			"expected_status":   row.ExpectedStatus,
			"expected_keyword":  row.ExpectedKeyword,
			"timeout_seconds":   row.TimeoutSeconds,
			"interval_seconds":  row.IntervalSeconds,
			"failure_threshold": row.FailureThreshold,
			"enabled":           row.Enabled,
			"service_id":        row.ServiceID,
			"config":            row.Config,
			"updated_at":        time.Now().UTC(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.First(&row, "id = ?", monitor.ID).Error; err != nil {
			return err
		}
		return replaceMonitorTags(ctx, tx, &row, monitor.Tags, orgID)
	})
	if err != nil {
		return models.Monitor{}, translateError(err)
	}
	return s.GetMonitor(ctx, monitor.ID)
}

func (s *PostgresStore) DeleteMonitor(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&dbMonitor{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) UpdateMonitorStatus(ctx context.Context, id string, status models.CheckStatus) error {
	if id == "" {
		return fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return err
	}
	q := s.db.WithContext(ctx).Model(&dbMonitor{}).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	result := q.Updates(map[string]any{
		"status":     string(status),
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func replaceMonitorTags(ctx context.Context, tx *gorm.DB, row *dbMonitor, tags []string, orgID string) error {
	tagRows := []dbTag{}
	for _, name := range normalizeTags(tags) {
		tag := dbTag{ID: newID(""), OrganizationID: orgID, Name: name, Color: "#888888"}
		if err := tx.WithContext(ctx).Where(dbTag{OrganizationID: orgID, Name: name}).FirstOrCreate(&tag).Error; err != nil {
			return err
		}
		tagRows = append(tagRows, tag)
	}
	return tx.WithContext(ctx).Model(row).Association("Tags").Replace(tagRows)
}

func monitorsFromRows(rows []dbMonitor) []models.Monitor {
	out := make([]models.Monitor, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out
}

func (s *PostgresStore) CreateCheckResult(ctx context.Context, result models.CheckResult) (models.CheckResult, error) {
	if result.CheckedAt.IsZero() {
		result.CheckedAt = time.Now().UTC()
	}
	if result.OrganizationID == "" && result.MonitorID != "" {
		orgID, err := s.organizationForMonitor(ctx, result.MonitorID)
		if err != nil {
			return models.CheckResult{}, err
		}
		result.OrganizationID = orgID
	}
	if result.Region == "" {
		result.Region = "default"
	}
	row := checkResultRow(result)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.CheckResult{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ListCheckResults(ctx context.Context, filter models.ResultFilter) ([]models.CheckResult, error) {
	limit := boundedLimit(filter.Limit, 100, 10000)
	q := s.checkResultQuery(ctx, filter).Order("checked_at DESC").Limit(limit)
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	var rows []dbCheckResult
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return checkResultsFromRows(rows), nil
}

func (s *PostgresStore) ExportCheckResults(ctx context.Context, filter models.ResultFilter) ([]models.CheckResult, error) {
	filter.Limit = boundedLimit(filter.Limit, 10000, 10000)
	return s.ListCheckResults(ctx, filter)
}

func (s *PostgresStore) checkResultQuery(ctx context.Context, filter models.ResultFilter) *gorm.DB {
	q := s.db.WithContext(ctx).Model(&dbCheckResult{})
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return q.Where("1 = 0")
	}
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if filter.MonitorID != "" {
		q = q.Where("monitor_id = ?", filter.MonitorID)
	}
	if filter.ServiceID != "" {
		sub := s.db.Model(&dbMonitor{}).Select("id").Where("service_id = ?", filter.ServiceID)
		q = q.Where("monitor_id IN (?)", sub)
	}
	if filter.StatusPageID != "" {
		sub := s.db.Model(&dbStatusPageComponentMonitor{}).
			Select("status_page_component_monitors.monitor_id").
			Joins("JOIN status_page_components ON status_page_components.id = status_page_component_monitors.component_id").
			Where("status_page_components.status_page_id = ?", filter.StatusPageID)
		q = q.Where("monitor_id IN (?)", sub)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.CheckedAfter != nil {
		q = q.Where("checked_at >= ?", *filter.CheckedAfter)
	}
	if filter.CheckedBefore != nil {
		q = q.Where("checked_at <= ?", *filter.CheckedBefore)
	}
	if filter.ExcludeMaintenance {
		q = q.Where("maintenance_suppressed = ?", false)
	}
	return q
}

func checkResultsFromRows(rows []dbCheckResult) []models.CheckResult {
	out := make([]models.CheckResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out
}

func boundedLimit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func (s *PostgresStore) CountConsecutiveFailures(ctx context.Context, monitorID string) (int, error) {
	if monitorID == "" {
		return 0, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	var rows []dbCheckResult
	if err := s.db.WithContext(ctx).Where("monitor_id = ?", monitorID).Order("checked_at DESC").Limit(50).Find(&rows).Error; err != nil {
		return 0, translateError(err)
	}
	count := 0
	for _, row := range rows {
		if row.Success {
			break
		}
		count++
	}
	return count, nil
}

func (s *PostgresStore) ListIncidents(ctx context.Context) ([]models.Incident, error) {
	var rows []dbIncident
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Order("started_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return incidentsFromRows(rows), nil
}

func (s *PostgresStore) GetIncident(ctx context.Context, id string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	var row dbIncident
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return row.toModel(), translateError(err)
}

func (s *PostgresStore) GetOpenIncident(ctx context.Context, monitorID string) (*models.Incident, error) {
	if monitorID == "" {
		return nil, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	var row dbIncident
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).
		Where("monitor_id = ? AND status IN ?", monitorID, activeIncidentStatuses).
		Order("started_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	model := row.toModel()
	return &model, nil
}

func (s *PostgresStore) OpenIncident(ctx context.Context, incident models.Incident) (models.Incident, error) {
	if incident.StartedAt.IsZero() {
		incident.StartedAt = time.Now().UTC()
	}
	if incident.OrganizationID == "" && incident.MonitorID != "" {
		orgID, err := s.organizationForMonitor(ctx, incident.MonitorID)
		if err != nil {
			return models.Incident{}, err
		}
		incident.OrganizationID = orgID
	}
	if incident.Status == "" {
		incident.Status = models.IncidentOpen
	}
	if incident.Severity == "" {
		incident.Severity = models.SeverityMajor
	}
	if incident.Impact == "" {
		incident.Impact = models.ImpactDegraded
	}
	row := incidentRow(incident)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.Incident{}, translateError(err)
	}
	created := row.toModel()
	_, _ = s.RecordIncidentTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID: created.ID,
		EventType:  "incident.opened",
		Message:    created.Reason,
		Metadata: map[string]any{
			"severity": created.Severity,
			"impact":   created.Impact,
			"groupKey": created.GroupKey,
		},
	})
	return created, nil
}

func (s *PostgresStore) ResolveIncident(ctx context.Context, id string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	now := time.Now().UTC()
	q := s.db.WithContext(ctx).Model(&dbIncident{}).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	result := q.Updates(map[string]any{
		"status":              string(models.IncidentResolved),
		"resolved_at":         &now,
		"resolved_by_user_id": "",
		"updated_at":          now,
	})
	if result.Error != nil {
		return models.Incident{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Incident{}, apierr.ErrNotFound
	}
	resolved, err := s.GetIncident(ctx, id)
	if err == nil {
		_, _ = s.RecordIncidentTimeline(ctx, models.IncidentTimelineEvent{IncidentID: id, EventType: "incident.resolved"})
	}
	return resolved, err
}

func (s *PostgresStore) ExportIncidents(ctx context.Context, filter models.ResultFilter) ([]models.Incident, error) {
	q := s.db.WithContext(ctx).Model(&dbIncident{}).Order("started_at DESC").Limit(boundedLimit(filter.Limit, 10000, 10000))
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if filter.MonitorID != "" {
		q = q.Where("monitor_id = ?", filter.MonitorID)
	}
	if filter.ServiceID != "" {
		sub := s.db.Model(&dbMonitor{}).Select("id").Where("service_id = ?", filter.ServiceID)
		q = q.Where("monitor_id IN (?)", sub)
	}
	if filter.StatusPageID != "" {
		sub := s.db.Model(&dbStatusPageComponentMonitor{}).
			Select("status_page_component_monitors.monitor_id").
			Joins("JOIN status_page_components ON status_page_components.id = status_page_component_monitors.component_id").
			Where("status_page_components.status_page_id = ?", filter.StatusPageID)
		q = q.Where("monitor_id IN (?)", sub)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.CheckedAfter != nil {
		q = q.Where("started_at >= ?", *filter.CheckedAfter)
	}
	if filter.CheckedBefore != nil {
		q = q.Where("started_at <= ?", *filter.CheckedBefore)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	var rows []dbIncident
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return incidentsFromRows(rows), nil
}

func incidentsFromRows(rows []dbIncident) []models.Incident {
	out := make([]models.Incident, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out
}

func (s *PostgresStore) ListNotificationChannels(ctx context.Context) ([]models.NotificationChannel, error) {
	var rows []dbNotificationChannel
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.NotificationChannel, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (s *PostgresStore) GetNotificationChannel(ctx context.Context, id string) (models.NotificationChannel, error) {
	if id == "" {
		return models.NotificationChannel{}, fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	var row dbNotificationChannel
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return row.toModel(), translateError(err)
}

func (s *PostgresStore) CreateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	channel.OrganizationID = orgID
	row := notificationChannelRow(channel)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.NotificationChannel{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) UpdateNotificationChannel(ctx context.Context, channel models.NotificationChannel) (models.NotificationChannel, error) {
	if channel.ID == "" {
		return models.NotificationChannel{}, fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	channel.OrganizationID = orgID
	row := notificationChannelRow(channel)
	result := s.db.WithContext(ctx).Model(&dbNotificationChannel{}).Where("id = ? AND organization_id = ?", channel.ID, orgID).Updates(map[string]any{
		"name":       row.Name,
		"type":       row.Type,
		"url":        row.URL,
		"config":     row.Config,
		"enabled":    row.Enabled,
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return models.NotificationChannel{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.NotificationChannel{}, apierr.ErrNotFound
	}
	return s.GetNotificationChannel(ctx, channel.ID)
}

func (s *PostgresStore) DeleteNotificationChannel(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: channel id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&dbNotificationChannel{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) LogNotificationEvent(ctx context.Context, channelID, incidentID, eventType string, success bool, statusCode int, errText string) error {
	row := dbNotificationEvent{
		ID:         newID(""),
		ChannelID:  stringPtr(channelID),
		IncidentID: stringPtr(incidentID),
		EventType:  eventType,
		Success:    success,
		StatusCode: statusCode,
		Error:      errText,
	}
	return translateError(s.db.WithContext(ctx).Create(&row).Error)
}

func (s *PostgresStore) CreateAPIKey(ctx context.Context, key models.APIKey) (models.APIKey, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.APIKey{}, err
	}
	row := dbAPIKey{ID: newID(key.ID), OrganizationID: orgID, Name: key.Name, KeyHash: key.KeyHash}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.APIKey{}, translateError(err)
	}
	return apiKeyModel(row), nil
}

func (s *PostgresStore) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	var rows []dbAPIKey
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.APIKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiKeyModel(row))
	}
	return out, nil
}

func (s *PostgresStore) FindAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error) {
	if hash == "" {
		return nil, nil
	}
	var row dbAPIKey
	err := s.db.WithContext(ctx).Where("key_hash = ? AND revoked_at IS NULL", hash).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	key := apiKeyModel(row)
	return &key, nil
}

func (s *PostgresStore) TouchAPIKey(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: api key id is required", apierr.ErrInvalidInput)
	}
	return translateError(s.db.WithContext(ctx).Model(&dbAPIKey{}).Where("id = ?", id).Update("last_used_at", time.Now().UTC()).Error)
}

func (s *PostgresStore) RevokeAPIKey(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: api key id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&dbAPIKey{}).Where("id = ? AND organization_id = ? AND revoked_at IS NULL", id, orgID).Update("revoked_at", time.Now().UTC())
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func apiKeyModel(row dbAPIKey) models.APIKey {
	return models.APIKey{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Name:           row.Name,
		KeyHash:        row.KeyHash,
		CreatedAt:      row.CreatedAt,
		LastUsedAt:     row.LastUsedAt,
		RevokedAt:      row.RevokedAt,
	}
}

func (s *PostgresStore) OverviewStats(ctx context.Context) (models.OverviewStats, error) {
	var stats models.OverviewStats
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return stats, err
	}
	monitorQ := s.db.WithContext(ctx).Model(&dbMonitor{})
	incidentQ := s.db.WithContext(ctx).Model(&dbIncident{})
	if !skip {
		monitorQ = monitorQ.Where("organization_id = ?", orgID)
		incidentQ = incidentQ.Where("organization_id = ?", orgID)
	}
	var total int64
	if err := monitorQ.Count(&total).Error; err != nil {
		return stats, translateError(err)
	}
	stats.TotalMonitors = int(total)
	counts := []struct {
		status string
		dst    *int
	}{
		{string(models.StatusUp), &stats.MonitorsUp},
		{string(models.StatusDown), &stats.MonitorsDown},
		{string(models.StatusDegraded), &stats.MonitorsDegraded},
	}
	for _, item := range counts {
		var count int64
		q := s.db.WithContext(ctx).Model(&dbMonitor{}).Where("status = ?", item.status)
		if !skip {
			q = q.Where("organization_id = ?", orgID)
		}
		if err := q.Count(&count).Error; err != nil {
			return stats, translateError(err)
		}
		*item.dst = int(count)
	}
	var openIncidents int64
	if err := incidentQ.Where("status = ?", string(models.IncidentOpen)).Count(&openIncidents).Error; err != nil {
		return stats, translateError(err)
	}
	stats.OpenIncidents = int(openIncidents)
	since := time.Now().UTC().Add(-24 * time.Hour)
	results, err := s.ListCheckResults(ctx, models.ResultFilter{CheckedAfter: &since, Limit: 10000})
	if err != nil {
		return stats, err
	}
	report := summarizeResults(results, since, time.Now().UTC())
	stats.UptimePercentage24H = report.UptimePercentage
	stats.AverageResponseMS = report.AverageResponseMS
	stats.P95ResponseMS = report.P95ResponseMS
	return stats, nil
}

func (s *PostgresStore) UptimeReport(ctx context.Context, filter models.UptimeReportFilter) (models.UptimeReport, error) {
	if filter.To.IsZero() {
		filter.To = time.Now().UTC()
	}
	if filter.From.IsZero() {
		filter.From = filter.To.Add(-24 * time.Hour)
	}
	resultFilter := models.ResultFilter{
		MonitorID:          filter.MonitorID,
		ServiceID:          filter.ServiceID,
		StatusPageID:       filter.StatusPageID,
		CheckedAfter:       &filter.From,
		CheckedBefore:      &filter.To,
		ExcludeMaintenance: filter.ExcludeMaintenance,
		Limit:              10000,
	}
	results, err := s.ListCheckResults(ctx, resultFilter)
	if err != nil {
		return models.UptimeReport{}, err
	}
	report := summarizeResults(results, filter.From, filter.To)
	report.MonitorID = filter.MonitorID
	report.ServiceID = filter.ServiceID
	report.StatusPageID = filter.StatusPageID
	incidents, err := s.ExportIncidents(ctx, models.ResultFilter{
		MonitorID:     filter.MonitorID,
		ServiceID:     filter.ServiceID,
		StatusPageID:  filter.StatusPageID,
		CheckedAfter:  &filter.From,
		CheckedBefore: &filter.To,
		Limit:         10000,
	})
	if err != nil {
		return models.UptimeReport{}, err
	}
	report.IncidentCount = len(incidents)
	return report, nil
}

func summarizeResults(results []models.CheckResult, from, to time.Time) models.UptimeReport {
	report := models.UptimeReport{From: from, To: to, GeneratedAt: time.Now().UTC()}
	report.Checks = len(results)
	if len(results) == 0 {
		return report
	}
	values := make([]float64, 0, len(results))
	var sum float64
	for _, result := range results {
		if result.Success {
			report.SuccessfulChecks++
		}
		ms := float64(result.ResponseTimeMS)
		sum += ms
		values = append(values, ms)
	}
	report.UptimePercentage = float64(report.SuccessfulChecks) / float64(report.Checks) * 100
	report.DowntimeMinutes = float64(report.Checks-report.SuccessfulChecks) * averageIntervalMinutes(results)
	report.AverageResponseMS = sum / float64(len(values))
	sort.Float64s(values)
	report.P50ResponseMS = percentile(values, 0.50)
	report.P95ResponseMS = percentile(values, 0.95)
	report.P99ResponseMS = percentile(values, 0.99)
	return report
}

func averageIntervalMinutes(results []models.CheckResult) float64 {
	if len(results) < 2 {
		return 0
	}
	times := make([]time.Time, 0, len(results))
	for _, result := range results {
		times = append(times, result.CheckedAt)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	var total time.Duration
	for i := 1; i < len(times); i++ {
		total += times[i].Sub(times[i-1])
	}
	return total.Minutes() / float64(len(times)-1)
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}
	rank := p * float64(len(values)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return values[lower]
	}
	weight := rank - float64(lower)
	return values[lower]*(1-weight) + values[upper]*weight
}

func (s *PostgresStore) UpsertWorkerHeartbeat(ctx context.Context, hb models.WorkerHeartbeat) error {
	if hb.InstanceID == "" {
		return fmt.Errorf("%w: instance id is required", apierr.ErrInvalidInput)
	}
	if hb.LastSeenAt.IsZero() {
		hb.LastSeenAt = time.Now().UTC()
	}
	row := dbWorkerHeartbeat{
		InstanceID:    hb.InstanceID,
		Hostname:      hb.Hostname,
		Version:       hb.Version,
		Region:        hb.Region,
		StartedAt:     hb.StartedAt,
		LastSeenAt:    hb.LastSeenAt,
		WorkerCount:   hb.WorkerCount,
		ActiveJobs:    hb.ActiveJobs,
		QueueDepth:    hb.QueueDepth,
		QueueCapacity: hb.QueueCapacity,
		JobsCompleted: hb.JobsCompleted,
		JobsFailed:    hb.JobsFailed,
		InFlight:      datatypes.NewJSONSlice(hb.InFlight),
	}
	return translateError(s.db.WithContext(ctx).Save(&row).Error)
}

func (s *PostgresStore) ListWorkerHeartbeats(ctx context.Context, since time.Time) ([]models.WorkerHeartbeat, error) {
	var rows []dbWorkerHeartbeat
	q := s.db.WithContext(ctx).Order("last_seen_at DESC")
	if !since.IsZero() {
		q = q.Where("last_seen_at >= ?", since)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.WorkerHeartbeat, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.WorkerHeartbeat{
			InstanceID:    row.InstanceID,
			Hostname:      row.Hostname,
			Version:       row.Version,
			Region:        row.Region,
			StartedAt:     row.StartedAt,
			LastSeenAt:    row.LastSeenAt,
			WorkerCount:   row.WorkerCount,
			ActiveJobs:    row.ActiveJobs,
			QueueDepth:    row.QueueDepth,
			QueueCapacity: row.QueueCapacity,
			JobsCompleted: row.JobsCompleted,
			JobsFailed:    row.JobsFailed,
			InFlight:      []string(row.InFlight),
		})
	}
	return out, nil
}

func (s *PostgresStore) DeleteWorkerHeartbeat(ctx context.Context, instanceID string) error {
	if instanceID == "" {
		return fmt.Errorf("%w: instance id is required", apierr.ErrInvalidInput)
	}
	return translateError(s.db.WithContext(ctx).Delete(&dbWorkerHeartbeat{}, "instance_id = ?", instanceID).Error)
}

func (s *PostgresStore) ListServices(ctx context.Context) ([]models.Service, error) {
	var rows []dbService
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Order("name ASC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.Service, 0, len(rows))
	for _, row := range rows {
		out = append(out, serviceModel(row))
	}
	return out, nil
}

func (s *PostgresStore) GetService(ctx context.Context, id string) (models.Service, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.Service{}, err
	}
	var row dbService
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return serviceModel(row), translateError(err)
}

func (s *PostgresStore) CreateService(ctx context.Context, service models.Service) (models.Service, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Service{}, err
	}
	row := dbService{ID: newID(service.ID), OrganizationID: orgID, Name: service.Name, Slug: service.Slug, Description: service.Description}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.Service{}, translateError(err)
	}
	return serviceModel(row), nil
}

func (s *PostgresStore) UpdateService(ctx context.Context, service models.Service) (models.Service, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Service{}, err
	}
	result := s.db.WithContext(ctx).Model(&dbService{}).Where("id = ? AND organization_id = ?", service.ID, orgID).Updates(map[string]any{
		"name":        service.Name,
		"slug":        service.Slug,
		"description": service.Description,
		"updated_at":  time.Now().UTC(),
	})
	if result.Error != nil {
		return models.Service{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Service{}, apierr.ErrNotFound
	}
	return s.GetService(ctx, service.ID)
}

func (s *PostgresStore) DeleteService(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&dbService{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListTags(ctx context.Context) ([]models.Tag, error) {
	var rows []dbTag
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Order("name ASC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.Tag, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.Tag{ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name, Color: row.Color, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func serviceModel(row dbService) models.Service {
	return models.Service{ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name, Slug: row.Slug, Description: row.Description, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s *PostgresStore) ListMaintenanceWindows(ctx context.Context) ([]models.MaintenanceWindow, error) {
	var rows []dbMaintenanceWindow
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Preload("Monitors").Preload("Tags").Order("starts_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return maintenanceWindowsFromRows(rows, time.Now().UTC()), nil
}

func (s *PostgresStore) GetMaintenanceWindow(ctx context.Context, id string) (models.MaintenanceWindow, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.MaintenanceWindow{}, err
	}
	var row dbMaintenanceWindow
	q := s.db.WithContext(ctx).Preload("Monitors").Preload("Tags").Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return row.toModel(time.Now().UTC()), translateError(err)
}

func (s *PostgresStore) CreateMaintenanceWindow(ctx context.Context, window models.MaintenanceWindow) (models.MaintenanceWindow, error) {
	if err := validateWindow(window); err != nil {
		return models.MaintenanceWindow{}, err
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.MaintenanceWindow{}, err
	}
	window.OrganizationID = orgID
	if !window.Enabled {
		window.Enabled = true
	}
	row := maintenanceWindowRow(window)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return replaceWindowTargets(ctx, tx, &row, window.MonitorIDs, window.TagNames)
	})
	if err != nil {
		return models.MaintenanceWindow{}, translateError(err)
	}
	return s.GetMaintenanceWindow(ctx, row.ID)
}

func (s *PostgresStore) UpdateMaintenanceWindow(ctx context.Context, window models.MaintenanceWindow) (models.MaintenanceWindow, error) {
	if window.ID == "" {
		return models.MaintenanceWindow{}, fmt.Errorf("%w: maintenance window id is required", apierr.ErrInvalidInput)
	}
	if err := validateWindow(window); err != nil {
		return models.MaintenanceWindow{}, err
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.MaintenanceWindow{}, err
	}
	window.OrganizationID = orgID
	row := maintenanceWindowRow(window)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&dbMaintenanceWindow{}).Where("id = ? AND organization_id = ?", window.ID, orgID).Updates(map[string]any{
			"name":               row.Name,
			"description":        row.Description,
			"starts_at":          row.StartsAt,
			"ends_at":            row.EndsAt,
			"timezone":           row.Timezone,
			"recurrence":         row.Recurrence,
			"recurrence_rrule":   row.RecurrenceRRule,
			"status_page_id":     row.StatusPageID,
			"created_by_user_id": row.CreatedByUserID,
			"enabled":            row.Enabled,
			"updated_at":         time.Now().UTC(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.First(&row, "id = ?", window.ID).Error; err != nil {
			return err
		}
		return replaceWindowTargets(ctx, tx, &row, window.MonitorIDs, window.TagNames)
	})
	if err != nil {
		return models.MaintenanceWindow{}, translateError(err)
	}
	return s.GetMaintenanceWindow(ctx, window.ID)
}

func (s *PostgresStore) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&dbMaintenanceWindow{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ActiveMaintenanceForMonitor(ctx context.Context, monitorID string, at time.Time) (*models.MaintenanceWindow, error) {
	monitor, err := s.GetMonitor(ctx, monitorID)
	if err != nil {
		return nil, err
	}
	windows, err := s.ListMaintenanceWindows(ctx)
	if err != nil {
		return nil, err
	}
	tagSet := map[string]bool{}
	for _, tag := range monitor.Tags {
		tagSet[tag] = true
	}
	for _, window := range windows {
		if !windowMatchesMonitor(window, monitorID, tagSet) {
			continue
		}
		if isWindowActive(window, at) {
			active := window
			active.Active = true
			return &active, nil
		}
	}
	return nil, nil
}

func validateWindow(window models.MaintenanceWindow) error {
	if !window.EndsAt.After(window.StartsAt) {
		return fmt.Errorf("%w: maintenance window endsAt must be after startsAt", apierr.ErrInvalidInput)
	}
	switch window.Recurrence {
	case "", "none", "daily", "weekly", "monthly":
		return nil
	default:
		return fmt.Errorf("%w: unsupported recurrence %q", apierr.ErrInvalidInput, window.Recurrence)
	}
}

func maintenanceWindowRow(window models.MaintenanceWindow) dbMaintenanceWindow {
	recurrence := window.Recurrence
	if recurrence == "" {
		recurrence = "none"
	}
	timezone := window.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	return dbMaintenanceWindow{
		ID:              newID(window.ID),
		OrganizationID:  window.OrganizationID,
		Name:            window.Name,
		Description:     window.Description,
		StartsAt:        window.StartsAt,
		EndsAt:          window.EndsAt,
		Timezone:        timezone,
		Recurrence:      recurrence,
		RecurrenceRRule: window.RecurrenceRRule,
		StatusPageID:    stringPtr(window.StatusPageID),
		CreatedByUserID: window.CreatedByUserID,
		Enabled:         window.Enabled,
	}
}

func replaceWindowTargets(ctx context.Context, tx *gorm.DB, row *dbMaintenanceWindow, monitorIDs, tagNames []string) error {
	monitorRows := []dbMonitor{}
	for _, id := range monitorIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		monitorRows = append(monitorRows, dbMonitor{ID: id})
	}
	tagRows := []dbTag{}
	for _, name := range normalizeTags(tagNames) {
		tag := dbTag{ID: newID(""), Name: name}
		if err := tx.WithContext(ctx).Where(dbTag{Name: name}).FirstOrCreate(&tag).Error; err != nil {
			return err
		}
		tagRows = append(tagRows, tag)
	}
	if err := tx.WithContext(ctx).Model(row).Association("Monitors").Replace(monitorRows); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(row).Association("Tags").Replace(tagRows)
}

func (w dbMaintenanceWindow) toModel(at time.Time) models.MaintenanceWindow {
	monitorIDs := make([]string, 0, len(w.Monitors))
	for _, monitor := range w.Monitors {
		monitorIDs = append(monitorIDs, monitor.ID)
	}
	tagNames := make([]string, 0, len(w.Tags))
	for _, tag := range w.Tags {
		tagNames = append(tagNames, tag.Name)
	}
	sort.Strings(monitorIDs)
	sort.Strings(tagNames)
	model := models.MaintenanceWindow{
		ID:              w.ID,
		OrganizationID:  w.OrganizationID,
		Name:            w.Name,
		Description:     w.Description,
		StartsAt:        w.StartsAt,
		EndsAt:          w.EndsAt,
		Timezone:        w.Timezone,
		Recurrence:      w.Recurrence,
		RecurrenceRRule: w.RecurrenceRRule,
		StatusPageID:    stringValue(w.StatusPageID),
		CreatedByUserID: w.CreatedByUserID,
		Enabled:         w.Enabled,
		MonitorIDs:      monitorIDs,
		TagNames:        tagNames,
		CreatedAt:       w.CreatedAt,
		UpdatedAt:       w.UpdatedAt,
	}
	model.Active = isWindowActive(model, at)
	return model
}

func maintenanceWindowsFromRows(rows []dbMaintenanceWindow, at time.Time) []models.MaintenanceWindow {
	out := make([]models.MaintenanceWindow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel(at))
	}
	return out
}

func windowMatchesMonitor(window models.MaintenanceWindow, monitorID string, tags map[string]bool) bool {
	if len(window.MonitorIDs) == 0 && len(window.TagNames) == 0 {
		return true
	}
	for _, id := range window.MonitorIDs {
		if id == monitorID {
			return true
		}
	}
	for _, tag := range window.TagNames {
		if tags[tag] {
			return true
		}
	}
	return false
}

func isWindowActive(window models.MaintenanceWindow, at time.Time) bool {
	if !window.Enabled {
		return false
	}
	loc := time.UTC
	if window.Timezone != "" {
		if loaded, err := time.LoadLocation(window.Timezone); err == nil {
			loc = loaded
		}
	}
	start := window.StartsAt.In(loc)
	end := window.EndsAt.In(loc)
	now := at.In(loc)
	duration := end.Sub(start)
	if duration <= 0 {
		return false
	}
	switch window.Recurrence {
	case "", "none":
		return !at.Before(window.StartsAt) && at.Before(window.EndsAt)
	case "daily":
		candidate := time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), loc)
		return !now.Before(candidate) && now.Before(candidate.Add(duration))
	case "weekly":
		days := (int(now.Weekday()) - int(start.Weekday()) + 7) % 7
		candidateDay := now.AddDate(0, 0, -days)
		candidate := time.Date(candidateDay.Year(), candidateDay.Month(), candidateDay.Day(), start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), loc)
		return !now.Before(candidate) && now.Before(candidate.Add(duration))
	case "monthly":
		if now.Day() != start.Day() {
			return false
		}
		candidate := time.Date(now.Year(), now.Month(), start.Day(), start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), loc)
		return !now.Before(candidate) && now.Before(candidate.Add(duration))
	default:
		return false
	}
}

func (s *PostgresStore) ListStatusPages(ctx context.Context) ([]models.StatusPage, error) {
	var rows []dbStatusPage
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Order("name ASC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.StatusPage, 0, len(rows))
	for _, row := range rows {
		out = append(out, statusPageModel(row))
	}
	return out, nil
}

func (s *PostgresStore) GetStatusPage(ctx context.Context, id string) (models.StatusPage, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.StatusPage{}, err
	}
	var row dbStatusPage
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return statusPageModel(row), translateError(err)
}

func (s *PostgresStore) GetStatusPageBySlug(ctx context.Context, slug string) (models.StatusPage, error) {
	var row dbStatusPage
	err := s.db.WithContext(ctx).Where("slug = ? AND (published = ? OR public = ?)", slug, true, true).First(&row).Error
	return statusPageModel(row), translateError(err)
}

func (s *PostgresStore) CreateStatusPage(ctx context.Context, page models.StatusPage) (models.StatusPage, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.StatusPage{}, err
	}
	row := dbStatusPage{
		ID:                   newID(page.ID),
		OrganizationID:       orgID,
		Slug:                 page.Slug,
		Name:                 page.Name,
		Description:          page.Description,
		CustomDomain:         page.CustomDomain,
		CustomDomainVerified: page.CustomDomainVerified,
		Theme:                jsonMap(page.Theme),
		Published:            page.Published,
		AutoUpdates:          page.AutoUpdates,
		LogoURL:              page.LogoURL,
		PrimaryColor:         page.PrimaryColor,
		Public:               page.Public,
		NoIndex:              page.NoIndex,
	}
	if !row.Public && row.Published {
		row.Public = true
	}
	if !row.Published && row.Public {
		row.Published = true
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.StatusPage{}, translateError(err)
	}
	return statusPageModel(row), nil
}

func (s *PostgresStore) UpdateStatusPage(ctx context.Context, page models.StatusPage) (models.StatusPage, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.StatusPage{}, err
	}
	result := s.db.WithContext(ctx).Model(&dbStatusPage{}).Where("id = ? AND organization_id = ?", page.ID, orgID).Updates(map[string]any{
		"slug":                   page.Slug,
		"name":                   page.Name,
		"description":            page.Description,
		"custom_domain":          page.CustomDomain,
		"custom_domain_verified": page.CustomDomainVerified,
		"theme":                  jsonMap(page.Theme),
		"published":              page.Published || page.Public,
		"auto_updates":           page.AutoUpdates,
		"logo_url":               page.LogoURL,
		"primary_color":          page.PrimaryColor,
		"public":                 page.Public || page.Published,
		"no_index":               page.NoIndex,
		"updated_at":             time.Now().UTC(),
	})
	if result.Error != nil {
		return models.StatusPage{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.StatusPage{}, apierr.ErrNotFound
	}
	return s.GetStatusPage(ctx, page.ID)
}

func (s *PostgresStore) DeleteStatusPage(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&dbStatusPage{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func statusPageModel(row dbStatusPage) models.StatusPage {
	return models.StatusPage{
		ID:                   row.ID,
		OrganizationID:       row.OrganizationID,
		Slug:                 row.Slug,
		Name:                 row.Name,
		Description:          row.Description,
		CustomDomain:         row.CustomDomain,
		CustomDomainVerified: row.CustomDomainVerified,
		Theme:                modelJSON(row.Theme),
		Published:            row.Published,
		AutoUpdates:          row.AutoUpdates,
		LogoURL:              row.LogoURL,
		PrimaryColor:         row.PrimaryColor,
		Public:               row.Public,
		NoIndex:              row.NoIndex,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}

func (s *PostgresStore) ListStatusPageComponents(ctx context.Context, statusPageID string) ([]models.StatusPageComponent, error) {
	var rows []dbStatusPageComponent
	if err := s.db.WithContext(ctx).Preload("Monitors").Where("status_page_id = ?", statusPageID).Order("order_index ASC, name ASC").Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return statusPageComponentsFromRows(rows), nil
}

func (s *PostgresStore) CreateStatusPageComponent(ctx context.Context, component models.StatusPageComponent) (models.StatusPageComponent, error) {
	row := statusPageComponentRow(component)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return replaceComponentMonitors(ctx, tx, &row, component.MonitorIDs)
	})
	if err != nil {
		return models.StatusPageComponent{}, translateError(err)
	}
	return s.getStatusPageComponent(ctx, row.ID)
}

func (s *PostgresStore) UpdateStatusPageComponent(ctx context.Context, component models.StatusPageComponent) (models.StatusPageComponent, error) {
	if component.ID == "" {
		return models.StatusPageComponent{}, fmt.Errorf("%w: component id is required", apierr.ErrInvalidInput)
	}
	row := statusPageComponentRow(component)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&dbStatusPageComponent{}).Where("id = ?", component.ID).Updates(map[string]any{
			"name":          row.Name,
			"description":   row.Description,
			"service_id":    row.ServiceID,
			"manual_status": row.ManualStatus,
			"order_index":   row.OrderIndex,
			"updated_at":    time.Now().UTC(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.First(&row, "id = ?", component.ID).Error; err != nil {
			return err
		}
		return replaceComponentMonitors(ctx, tx, &row, component.MonitorIDs)
	})
	if err != nil {
		return models.StatusPageComponent{}, translateError(err)
	}
	return s.getStatusPageComponent(ctx, component.ID)
}

func (s *PostgresStore) DeleteStatusPageComponent(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Delete(&dbStatusPageComponent{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) getStatusPageComponent(ctx context.Context, id string) (models.StatusPageComponent, error) {
	var row dbStatusPageComponent
	err := s.db.WithContext(ctx).Preload("Monitors").First(&row, "id = ?", id).Error
	return row.toModel(), translateError(err)
}

func statusPageComponentRow(component models.StatusPageComponent) dbStatusPageComponent {
	return dbStatusPageComponent{
		ID:           newID(component.ID),
		StatusPageID: component.StatusPageID,
		Name:         component.Name,
		Description:  component.Description,
		Position:     component.Position,
		GroupName:    component.GroupName,
		ServiceID:    stringPtr(component.ServiceID),
		ManualStatus: string(component.ManualStatus),
		OrderIndex:   component.OrderIndex,
	}
}

func replaceComponentMonitors(ctx context.Context, tx *gorm.DB, row *dbStatusPageComponent, monitorIDs []string) error {
	monitorRows := []dbMonitor{}
	for _, id := range monitorIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		monitorRows = append(monitorRows, dbMonitor{ID: id})
	}
	return tx.WithContext(ctx).Model(row).Association("Monitors").Replace(monitorRows)
}

func (c dbStatusPageComponent) toModel() models.StatusPageComponent {
	monitorIDs := make([]string, 0, len(c.Monitors))
	for _, monitor := range c.Monitors {
		monitorIDs = append(monitorIDs, monitor.ID)
	}
	sort.Strings(monitorIDs)
	return models.StatusPageComponent{
		ID:           c.ID,
		StatusPageID: c.StatusPageID,
		Name:         c.Name,
		Description:  c.Description,
		Position:     c.Position,
		GroupName:    c.GroupName,
		ServiceID:    stringValue(c.ServiceID),
		ManualStatus: models.CheckStatus(c.ManualStatus),
		OrderIndex:   c.OrderIndex,
		MonitorIDs:   monitorIDs,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

func statusPageComponentsFromRows(rows []dbStatusPageComponent) []models.StatusPageComponent {
	out := make([]models.StatusPageComponent, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out
}

func (s *PostgresStore) PublicStatusPage(ctx context.Context, slug string) (models.PublicStatusPage, error) {
	page, err := s.GetStatusPageBySlug(ctx, slug)
	if err != nil {
		return models.PublicStatusPage{}, err
	}
	components, err := s.ListStatusPageComponents(ctx, page.ID)
	if err != nil {
		return models.PublicStatusPage{}, err
	}
	overall := models.StatusUp
	for i := range components {
		status, err := s.componentStatus(ctx, components[i])
		if err != nil {
			return models.PublicStatusPage{}, err
		}
		components[i].Status = status
		overall = worstStatus(overall, status)
	}
	active, err := s.ExportIncidents(ctx, models.ResultFilter{StatusPageID: page.ID, Status: string(models.IncidentOpen), Limit: 25})
	if err != nil {
		return models.PublicStatusPage{}, err
	}
	recent, err := s.ExportIncidents(ctx, models.ResultFilter{StatusPageID: page.ID, Limit: 25})
	if err != nil {
		return models.PublicStatusPage{}, err
	}
	report, err := s.UptimeReport(ctx, models.UptimeReportFilter{StatusPageID: page.ID, From: time.Now().UTC().Add(-24 * time.Hour), To: time.Now().UTC()})
	if err != nil {
		return models.PublicStatusPage{}, err
	}
	return models.PublicStatusPage{
		Page:            page,
		Status:          overall,
		Components:      components,
		ActiveIncidents: active,
		RecentIncidents: recent,
		Uptime24H:       report.UptimePercentage,
		GeneratedAt:     time.Now().UTC(),
	}, nil
}

func (s *PostgresStore) componentStatus(ctx context.Context, component models.StatusPageComponent) (models.CheckStatus, error) {
	if component.ManualStatus != "" {
		return component.ManualStatus, nil
	}
	status := models.StatusUp
	if component.ServiceID != "" {
		monitors, err := s.ListMonitorsFiltered(ctx, models.MonitorFilter{ServiceID: component.ServiceID})
		if err != nil {
			return status, err
		}
		for _, monitor := range monitors {
			status = worstStatus(status, monitor.Status)
		}
	}
	for _, id := range component.MonitorIDs {
		monitor, err := s.GetMonitor(ctx, id)
		if err != nil {
			return status, err
		}
		status = worstStatus(status, monitor.Status)
	}
	return status, nil
}

func worstStatus(current, next models.CheckStatus) models.CheckStatus {
	if current == models.StatusDown || next == models.StatusDown {
		return models.StatusDown
	}
	if current == models.StatusDegraded || next == models.StatusDegraded {
		return models.StatusDegraded
	}
	return models.StatusUp
}

func (s *PostgresStore) FindHeartbeatMonitorByTokenHash(ctx context.Context, tokenHash string) (*models.Monitor, error) {
	if tokenHash == "" {
		return nil, nil
	}
	var row dbMonitor
	err := s.db.WithContext(ctx).
		Preload("Tags").
		Where("type = ?", string(models.MonitorHeartbeat)).
		Where(datatypes.JSONQuery("config").Equals(tokenHash, "heartbeatTokenHash")).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	model := row.toModel()
	return &model, nil
}

func (s *PostgresStore) RecordHeartbeat(ctx context.Context, event models.HeartbeatEvent) (models.HeartbeatEvent, error) {
	row := dbHeartbeatEvent{
		ID:         newID(event.ID),
		MonitorID:  event.MonitorID,
		Status:     string(event.Status),
		Message:    event.Message,
		DurationMS: event.DurationMS,
		Metadata:   jsonMap(event.Metadata),
		CreatedAt:  event.CreatedAt,
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.HeartbeatEvent{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) LastHeartbeat(ctx context.Context, monitorID string) (*models.HeartbeatEvent, error) {
	var row dbHeartbeatEvent
	err := s.db.WithContext(ctx).Where("monitor_id = ?", monitorID).Order("created_at DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	model := row.toModel()
	return &model, nil
}

func (h dbHeartbeatEvent) toModel() models.HeartbeatEvent {
	return models.HeartbeatEvent{
		ID:         h.ID,
		MonitorID:  h.MonitorID,
		Status:     models.CheckStatus(h.Status),
		Message:    h.Message,
		DurationMS: h.DurationMS,
		Metadata:   modelJSON(h.Metadata),
		CreatedAt:  h.CreatedAt,
	}
}

// Ensure the underlying connection pool can be tuned and closed by callers
// that need direct lifecycle control in tests.
func SQLDB(db *gorm.DB) (*sql.DB, error) {
	if db == nil {
		return nil, errors.New("nil gorm db")
	}
	return db.DB()
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (s *PostgresStore) organizationForMonitor(ctx context.Context, monitorID string) (string, error) {
	var row dbMonitor
	err := s.db.WithContext(ctx).Select("organization_id").First(&row, "id = ?", monitorID).Error
	if err != nil {
		return "", translateError(err)
	}
	return row.OrganizationID, nil
}

func (s *PostgresStore) AcknowledgeIncident(ctx context.Context, id, userID string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&dbIncident{}).
		Where("id = ? AND organization_id = ?", id, orgID).
		Updates(map[string]any{
			"status":                  string(models.IncidentAcknowledged),
			"acknowledged_at":         &now,
			"acknowledged_by_user_id": userID,
			"updated_at":              now,
		})
	if result.Error != nil {
		return models.Incident{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Incident{}, apierr.ErrNotFound
	}
	incident, err := s.GetIncident(ctx, id)
	if err == nil {
		_, _ = s.RecordIncidentTimeline(ctx, models.IncidentTimelineEvent{IncidentID: id, EventType: "incident.acknowledged", ActorUserID: userID})
	}
	return incident, err
}

func (s *PostgresStore) CreateTag(ctx context.Context, t models.Tag) (models.Tag, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Tag{}, err
	}
	if t.Color == "" {
		t.Color = "#888888"
	}
	row := dbTag{ID: newID(t.ID), OrganizationID: orgID, Name: strings.ToLower(strings.TrimSpace(t.Name)), Color: t.Color}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.Tag{}, translateError(err)
	}
	return models.Tag{ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name, Color: row.Color, CreatedAt: row.CreatedAt}, nil
}

func (s *PostgresStore) DeleteTag(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&dbTag{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) SetMonitorTags(ctx context.Context, monitorID string, tagIDs []string) error {
	if monitorID == "" {
		return fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	monitor, err := s.GetMonitor(ctx, monitorID)
	if err != nil {
		return err
	}
	var tags []dbTag
	if len(tagIDs) > 0 {
		if err := s.db.WithContext(ctx).
			Where("organization_id = ? AND id IN ?", monitor.OrganizationID, tagIDs).
			Find(&tags).Error; err != nil {
			return translateError(err)
		}
	}
	row := dbMonitor{ID: monitorID}
	return translateError(s.db.WithContext(ctx).Model(&row).Association("Tags").Replace(tags))
}

func (s *PostgresStore) ListMonitorsByTags(ctx context.Context, names []string) ([]models.Monitor, error) {
	normalized := normalizeTags(names)
	if len(normalized) == 0 {
		return s.ListMonitors(ctx)
	}
	monitors, err := s.ListMonitorsFiltered(ctx, models.MonitorFilter{})
	if err != nil {
		return nil, err
	}
	required := map[string]bool{}
	for _, name := range normalized {
		required[name] = true
	}
	out := make([]models.Monitor, 0, len(monitors))
	for _, monitor := range monitors {
		seen := map[string]bool{}
		for _, tag := range monitor.Tags {
			seen[strings.ToLower(tag)] = true
		}
		match := true
		for name := range required {
			if !seen[name] {
				match = false
				break
			}
		}
		if match {
			out = append(out, monitor)
		}
	}
	return out, nil
}

func (s *PostgresStore) GetMonitorsByIDs(ctx context.Context, organizationID string, ids []string) ([]models.Monitor, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []dbMonitor
	q := s.db.WithContext(ctx).Preload("Tags").Where("id IN ?", ids)
	if organizationID != "" {
		q = q.Where("organization_id = ?", organizationID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return monitorsFromRows(rows), nil
}

func (s *PostgresStore) GetStatusPageByDomain(ctx context.Context, domain string) (models.StatusPage, error) {
	if strings.TrimSpace(domain) == "" {
		return models.StatusPage{}, apierr.ErrNotFound
	}
	var row dbStatusPage
	err := s.db.WithContext(ctx).
		Where("custom_domain = ? AND custom_domain_verified = ? AND (published = ? OR public = ?)", domain, true, true, true).
		First(&row).Error
	return statusPageModel(row), translateError(err)
}

func (s *PostgresStore) UpsertStatusPageComponent(ctx context.Context, c models.StatusPageComponent) (models.StatusPageComponent, error) {
	if c.ID == "" {
		return s.CreateStatusPageComponent(ctx, c)
	}
	return s.UpdateStatusPageComponent(ctx, c)
}

func (s *PostgresStore) IsMonitorInMaintenance(ctx context.Context, monitorID string, at time.Time) (bool, error) {
	active, err := s.ActiveMaintenanceForMonitor(ctx, monitorID, at)
	return active != nil, err
}

func (s *PostgresStore) GetOrganization(ctx context.Context, id string) (models.Organization, error) {
	if id == "" {
		return models.Organization{}, fmt.Errorf("%w: organization id is required", apierr.ErrInvalidInput)
	}
	var row dbOrganization
	err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return organizationModel(row), translateError(err)
}

func (s *PostgresStore) GetOrganizationByClerkID(ctx context.Context, clerkOrgID string) (models.Organization, error) {
	if clerkOrgID == "" {
		return models.Organization{}, fmt.Errorf("%w: clerk org id is required", apierr.ErrInvalidInput)
	}
	var row dbOrganization
	err := s.db.WithContext(ctx).First(&row, "clerk_org_id = ?", clerkOrgID).Error
	return organizationModel(row), translateError(err)
}

func (s *PostgresStore) UpsertOrganization(ctx context.Context, org models.Organization) (models.Organization, error) {
	if org.Plan == "" {
		org.Plan = "free"
	}
	row := dbOrganization{
		ID:         newID(org.ID),
		ClerkOrgID: org.ClerkOrgID,
		Name:       org.Name,
		Slug:       org.Slug,
		Plan:       org.Plan,
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "clerk_org_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "slug", "plan", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return models.Organization{}, translateError(err)
	}
	if org.ClerkOrgID != "" {
		return s.GetOrganizationByClerkID(ctx, org.ClerkOrgID)
	}
	return s.GetOrganization(ctx, row.ID)
}

func (s *PostgresStore) DeleteOrganizationByClerkID(ctx context.Context, clerkOrgID string) error {
	return translateError(s.db.WithContext(ctx).Delete(&dbOrganization{}, "clerk_org_id = ?", clerkOrgID).Error)
}

func organizationModel(row dbOrganization) models.Organization {
	return models.Organization{ID: row.ID, ClerkOrgID: row.ClerkOrgID, Name: row.Name, Slug: row.Slug, Plan: row.Plan, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id string) (models.User, error) {
	var row dbUser
	err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return userModel(row), translateError(err)
}

func (s *PostgresStore) GetUserByClerkID(ctx context.Context, clerkUserID string) (models.User, error) {
	var row dbUser
	err := s.db.WithContext(ctx).First(&row, "clerk_user_id = ?", clerkUserID).Error
	return userModel(row), translateError(err)
}

func (s *PostgresStore) UpsertUser(ctx context.Context, user models.User) (models.User, error) {
	row := dbUser{ID: newID(user.ID), ClerkUserID: user.ClerkUserID, Email: user.Email, Name: user.Name, ImageURL: user.ImageURL}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "clerk_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"email", "name", "image_url", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return models.User{}, translateError(err)
	}
	if user.ClerkUserID != "" {
		return s.GetUserByClerkID(ctx, user.ClerkUserID)
	}
	return s.GetUserByID(ctx, row.ID)
}

func (s *PostgresStore) DeleteUserByClerkID(ctx context.Context, clerkUserID string) error {
	return translateError(s.db.WithContext(ctx).Delete(&dbUser{}, "clerk_user_id = ?", clerkUserID).Error)
}

func userModel(row dbUser) models.User {
	return models.User{ID: row.ID, ClerkUserID: row.ClerkUserID, Email: row.Email, Name: row.Name, ImageURL: row.ImageURL, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s *PostgresStore) ListMembershipsForUser(ctx context.Context, userID string) ([]models.MembershipDetail, error) {
	var rows []dbMembership
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.MembershipDetail, 0, len(rows))
	for _, row := range rows {
		org, err := s.GetOrganization(ctx, row.OrganizationID)
		if err != nil {
			return nil, err
		}
		out = append(out, models.MembershipDetail{
			OrganizationID:   org.ID,
			OrganizationName: org.Name,
			OrganizationSlug: org.Slug,
			Plan:             org.Plan,
			Role:             row.Role,
		})
	}
	return out, nil
}

func (s *PostgresStore) UpsertMembership(ctx context.Context, m models.Membership) error {
	if m.OrganizationID == "" || m.UserID == "" {
		return fmt.Errorf("%w: organization id and user id are required", apierr.ErrInvalidInput)
	}
	if m.Role == "" {
		m.Role = "member"
	}
	row := dbMembership{OrganizationID: m.OrganizationID, UserID: m.UserID, Role: m.Role}
	return translateError(s.db.WithContext(ctx).Save(&row).Error)
}

func (s *PostgresStore) DeleteMembership(ctx context.Context, organizationID, userID string) error {
	return translateError(s.db.WithContext(ctx).Delete(&dbMembership{}, "organization_id = ? AND user_id = ?", organizationID, userID).Error)
}

func (s *PostgresStore) RecordWebhookEvent(ctx context.Context, id, source string, payload []byte) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("%w: webhook event id is required", apierr.ErrInvalidInput)
	}
	data := map[string]any{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &data)
	}
	row := dbWebhookEvent{ID: id, Source: source, Payload: jsonMap(data)}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if result.Error != nil {
		return false, translateError(result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (s *PostgresStore) GetHeartbeat(ctx context.Context, monitorID string) (models.Heartbeat, error) {
	var row dbHeartbeat
	err := s.db.WithContext(ctx).First(&row, "monitor_id = ?", monitorID).Error
	return heartbeatModel(row), translateError(err)
}

func (s *PostgresStore) SetHeartbeat(ctx context.Context, hb models.Heartbeat) (models.Heartbeat, error) {
	if hb.MonitorID == "" {
		return models.Heartbeat{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	if hb.ExpectedIntervalSeconds <= 0 {
		hb.ExpectedIntervalSeconds = 60
	}
	if hb.GraceSeconds < 0 {
		hb.GraceSeconds = 0
	}
	row := dbHeartbeat{
		MonitorID:               hb.MonitorID,
		TokenHash:               hb.TokenHash,
		ExpectedIntervalSeconds: hb.ExpectedIntervalSeconds,
		GraceSeconds:            hb.GraceSeconds,
		LastPingAt:              hb.LastPingAt,
		LastPingSourceIP:        hb.LastPingSourceIP,
		LastPingUserAgent:       hb.LastPingUserAgent,
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return models.Heartbeat{}, translateError(err)
	}
	return s.GetHeartbeat(ctx, hb.MonitorID)
}

func (s *PostgresStore) DeleteHeartbeat(ctx context.Context, monitorID string) error {
	return translateError(s.db.WithContext(ctx).Delete(&dbHeartbeat{}, "monitor_id = ?", monitorID).Error)
}

func (s *PostgresStore) RecordHeartbeatPing(ctx context.Context, tokenHash, sourceIP, userAgent string) (string, error) {
	if tokenHash == "" {
		return "", fmt.Errorf("%w: token is required", apierr.ErrInvalidInput)
	}
	var row dbHeartbeat
	err := s.db.WithContext(ctx).First(&row, "token_hash = ?", tokenHash).Error
	if err != nil {
		return "", translateError(err)
	}
	now := time.Now().UTC()
	err = s.db.WithContext(ctx).Model(&dbHeartbeat{}).Where("monitor_id = ?", row.MonitorID).Updates(map[string]any{
		"last_ping_at":         &now,
		"last_ping_source_ip":  sourceIP,
		"last_ping_user_agent": userAgent,
		"updated_at":           now,
	}).Error
	return row.MonitorID, translateError(err)
}

func heartbeatModel(row dbHeartbeat) models.Heartbeat {
	return models.Heartbeat{
		MonitorID:               row.MonitorID,
		TokenHash:               row.TokenHash,
		ExpectedIntervalSeconds: row.ExpectedIntervalSeconds,
		GraceSeconds:            row.GraceSeconds,
		LastPingAt:              row.LastPingAt,
		LastPingSourceIP:        row.LastPingSourceIP,
		LastPingUserAgent:       row.LastPingUserAgent,
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

func (s *PostgresStore) GetMultistepScript(ctx context.Context, monitorID string) (models.MultistepScript, error) {
	var row dbMultistepScript
	err := s.db.WithContext(ctx).First(&row, "monitor_id = ?", monitorID).Error
	if err != nil {
		return models.MultistepScript{}, translateError(err)
	}
	return models.MultistepScript{MonitorID: row.MonitorID, Steps: multistepSteps(row.Steps), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func (s *PostgresStore) SetMultistepScript(ctx context.Context, script models.MultistepScript) (models.MultistepScript, error) {
	row := dbMultistepScript{MonitorID: script.MonitorID, Steps: multistepJSON(script.Steps)}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return models.MultistepScript{}, translateError(err)
	}
	return s.GetMultistepScript(ctx, script.MonitorID)
}

func multistepJSON(steps models.MultistepSteps) datatypes.JSONMap {
	data := map[string]any{}
	b, _ := json.Marshal(steps)
	_ = json.Unmarshal(b, &data)
	return data
}

func multistepSteps(data datatypes.JSONMap) models.MultistepSteps {
	var steps models.MultistepSteps
	b, _ := json.Marshal(modelJSON(data))
	_ = json.Unmarshal(b, &steps)
	return steps
}

func (s *PostgresStore) GetBrowserScript(ctx context.Context, monitorID string) (models.BrowserScript, error) {
	var row dbBrowserScript
	err := s.db.WithContext(ctx).First(&row, "monitor_id = ?", monitorID).Error
	return models.BrowserScript{
		MonitorID:      row.MonitorID,
		Source:         row.Source,
		TimeoutSeconds: row.TimeoutSeconds,
		Retries:        row.Retries,
		Env:            redactedJSON(row.Env),
		RetentionDays:  row.RetentionDays,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, translateError(err)
}

func (s *PostgresStore) SetBrowserScript(ctx context.Context, script models.BrowserScript) (models.BrowserScript, error) {
	row := dbBrowserScript{
		MonitorID:      script.MonitorID,
		Source:         script.Source,
		TimeoutSeconds: script.TimeoutSeconds,
		Retries:        script.Retries,
		Env:            jsonMap(script.Env),
		RetentionDays:  script.RetentionDays,
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return models.BrowserScript{}, translateError(err)
	}
	return s.GetBrowserScript(ctx, script.MonitorID)
}

func (s *PostgresStore) EnqueueNotification(ctx context.Context, entry models.OutboxEntry) (models.OutboxEntry, error) {
	if entry.OrganizationID == "" {
		return models.OutboxEntry{}, fmt.Errorf("%w: organization id is required", apierr.ErrInvalidInput)
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
	row := dbOutboxEntry{
		ID:             newID(entry.ID),
		OrganizationID: entry.OrganizationID,
		ChannelID:      stringPtr(entry.ChannelID),
		IncidentID:     stringPtr(entry.IncidentID),
		EventType:      entry.EventType,
		Payload:        entry.Payload,
		Attempts:       entry.Attempts,
		NextAttemptAt:  entry.NextAttemptAt,
		Status:         entry.Status,
		LastError:      entry.LastError,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.OutboxEntry{}, translateError(err)
	}
	return outboxModel(row), nil
}

func (s *PostgresStore) ClaimPendingNotifications(ctx context.Context, limit int) ([]models.OutboxEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []dbOutboxEntry
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_attempt_at <= ?", "pending", time.Now().UTC()).
			Order("next_attempt_at ASC").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		return tx.Model(&dbOutboxEntry{}).Where("id IN ?", ids).Updates(map[string]any{
			"status":     "processing",
			"updated_at": time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return nil, translateError(err)
	}
	out := make([]models.OutboxEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, outboxModel(row))
	}
	return out, nil
}

func (s *PostgresStore) MarkNotificationDelivered(ctx context.Context, id string) error {
	return translateError(s.db.WithContext(ctx).Model(&dbOutboxEntry{}).Where("id = ?", id).Updates(map[string]any{
		"status":     "delivered",
		"updated_at": time.Now().UTC(),
	}).Error)
}

func (s *PostgresStore) MarkNotificationRetry(ctx context.Context, id string, attempts, maxAttempts int, lastErr string, next time.Time) error {
	status := "pending"
	if attempts >= maxAttempts {
		status = "failed"
	}
	return translateError(s.db.WithContext(ctx).Model(&dbOutboxEntry{}).Where("id = ?", id).Updates(map[string]any{
		"attempts":        attempts,
		"next_attempt_at": next,
		"status":          status,
		"last_error":      lastErr,
		"updated_at":      time.Now().UTC(),
	}).Error)
}

func outboxModel(row dbOutboxEntry) models.OutboxEntry {
	return models.OutboxEntry{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		ChannelID:      stringValue(row.ChannelID),
		IncidentID:     stringValue(row.IncidentID),
		EventType:      row.EventType,
		Payload:        row.Payload,
		Attempts:       row.Attempts,
		NextAttemptAt:  row.NextAttemptAt,
		Status:         row.Status,
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func (s *PostgresStore) UpsertPushDevice(ctx context.Context, device models.PushDevice) (models.PushDevice, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.PushDevice{}, err
	}
	row := dbPushDevice{
		ID:             newID(device.ID),
		OrganizationID: orgID,
		UserID:         device.UserID,
		Platform:       device.Platform,
		ExpoToken:      device.ExpoToken,
		AppVersion:     device.AppVersion,
		LastSeenAt:     time.Now().UTC(),
	}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "expo_token"}},
		DoUpdates: clause.AssignmentColumns([]string{"organization_id", "user_id", "platform", "app_version", "last_seen_at"}),
	}).Create(&row).Error
	if err != nil {
		return models.PushDevice{}, translateError(err)
	}
	return pushDeviceModel(row), nil
}

func (s *PostgresStore) DeletePushDevice(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&dbPushDevice{}, "id = ?", id)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListPushDevicesForOrg(ctx context.Context, organizationID string) ([]models.PushDevice, error) {
	var rows []dbPushDevice
	if err := s.db.WithContext(ctx).Where("organization_id = ?", organizationID).Order("last_seen_at DESC").Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return pushDevicesFromRows(rows), nil
}

func (s *PostgresStore) ListPushDevicesForUser(ctx context.Context, userID string) ([]models.PushDevice, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("last_seen_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	var rows []dbPushDevice
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	return pushDevicesFromRows(rows), nil
}

func pushDeviceModel(row dbPushDevice) models.PushDevice {
	return models.PushDevice{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		UserID:         row.UserID,
		Platform:       row.Platform,
		ExpoToken:      row.ExpoToken,
		AppVersion:     row.AppVersion,
		LastSeenAt:     row.LastSeenAt,
		CreatedAt:      row.CreatedAt,
	}
}

func pushDevicesFromRows(rows []dbPushDevice) []models.PushDevice {
	out := make([]models.PushDevice, 0, len(rows))
	for _, row := range rows {
		out = append(out, pushDeviceModel(row))
	}
	return out
}

func (s *PostgresStore) SLAReportForMonitor(ctx context.Context, monitorID string, from, to time.Time) (models.SLAReport, error) {
	if !to.After(from) {
		return models.SLAReport{}, fmt.Errorf("%w: 'to' must be after 'from'", apierr.ErrInvalidInput)
	}
	filter := models.ResultFilter{MonitorID: monitorID, CheckedAfter: &from, CheckedBefore: &to, Limit: 10000}
	results, err := s.ListCheckResults(ctx, filter)
	if err != nil {
		return models.SLAReport{}, err
	}
	report := models.SLAReport{MonitorID: monitorID, From: from.UTC(), To: to.UTC(), UptimePercentage: 100}
	if len(results) > 0 {
		up := 0
		for _, result := range results {
			if result.Success {
				up++
			}
		}
		report.UptimePercentage = float64(up) / float64(len(results)) * 100
	}
	incidents, err := s.ExportIncidents(ctx, models.ResultFilter{MonitorID: monitorID, CheckedAfter: &from, CheckedBefore: &to, Limit: 10000})
	if err != nil {
		return report, err
	}
	report.IncidentCount = len(incidents)
	for _, incident := range incidents {
		end := to
		if incident.ResolvedAt != nil && incident.ResolvedAt.Before(end) {
			end = *incident.ResolvedAt
		}
		start := incident.StartedAt
		if start.Before(from) {
			start = from
		}
		if end.After(start) {
			report.RawDownSeconds += int64(end.Sub(start).Seconds())
		}
	}
	report.BillableDownSeconds = report.RawDownSeconds
	return report, nil
}

func (s *PostgresStore) SLAReportForOrg(ctx context.Context, from, to time.Time) (models.SLAReport, error) {
	monitors, err := s.ListMonitors(ctx)
	if err != nil {
		return models.SLAReport{}, err
	}
	report := models.SLAReport{From: from.UTC(), To: to.UTC(), UptimePercentage: 100}
	if len(monitors) == 0 {
		return report, nil
	}
	var pctSum float64
	for _, monitor := range monitors {
		sub, err := s.SLAReportForMonitor(ctx, monitor.ID, from, to)
		if err != nil {
			return report, err
		}
		pctSum += sub.UptimePercentage
		report.RawDownSeconds += sub.RawDownSeconds
		report.MaintenanceSeconds += sub.MaintenanceSeconds
		report.BillableDownSeconds += sub.BillableDownSeconds
		report.IncidentCount += sub.IncidentCount
	}
	report.UptimePercentage = pctSum / float64(len(monitors))
	return report, nil
}

func SplitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
