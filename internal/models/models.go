package models

import "time"

type MonitorType string
type CheckStatus string
type IncidentStatus string
type IncidentSeverity string
type IncidentImpact string

const (
	MonitorHTTP      MonitorType = "http"
	MonitorAPI       MonitorType = "api"
	MonitorTCP       MonitorType = "tcp"
	MonitorUDP       MonitorType = "udp"
	MonitorDNS       MonitorType = "dns"
	MonitorTLS       MonitorType = "tls"
	MonitorKeyword   MonitorType = "keyword"
	MonitorHeartbeat MonitorType = "heartbeat"
	MonitorICMP      MonitorType = "icmp"
	MonitorPing      MonitorType = "ping"
	MonitorBrowser   MonitorType = "browser"
	MonitorDomain    MonitorType = "domain"
	MonitorMultistep MonitorType = "multistep"

	StatusUp       CheckStatus = "up"
	StatusDown     CheckStatus = "down"
	StatusDegraded CheckStatus = "degraded"

	IncidentOpen          IncidentStatus = "open"
	IncidentAcknowledged  IncidentStatus = "acknowledged"
	IncidentInvestigating IncidentStatus = "investigating"
	IncidentIdentified    IncidentStatus = "identified"
	IncidentMonitoring    IncidentStatus = "monitoring"
	IncidentResolved      IncidentStatus = "resolved"

	SeverityInfo     IncidentSeverity = "info"
	SeverityWarning  IncidentSeverity = "warning"
	SeverityMinor    IncidentSeverity = "minor"
	SeverityMajor    IncidentSeverity = "major"
	SeverityCritical IncidentSeverity = "critical"

	ImpactNone          IncidentImpact = "none"
	ImpactDegraded      IncidentImpact = "degraded"
	ImpactPartialOutage IncidentImpact = "partial_outage"
	ImpactFullOutage    IncidentImpact = "full_outage"
)

// Monitor is the canonical synthetic-check definition. Status, CreatedAt,
// and UpdatedAt are server-managed; clients can submit them but the API
// layer zeroes Status on create so users cannot manufacture "up" state.
// OrganizationID is filled in by the repository from the request principal
// — clients cannot set it on create or update.
type Monitor struct {
	ID             string      `json:"id"`
	OrganizationID string      `json:"organizationId"`
	Name           string      `json:"name" binding:"required,min=1,max=120"`
	Type           MonitorType `json:"type" binding:"required,oneof=http api tcp udp dns tls keyword heartbeat icmp ping browser domain multistep"`
	// Target is required for network-based checks (HTTP/TCP/DNS/TLS/ICMP/
	// Domain). Heartbeat, Browser, and Multistep monitors store their
	// configuration in dedicated tables; the binding tag is intentionally
	// loose here and the API handler enforces per-type rules.
	Target           string         `json:"target" binding:"omitempty,max=2048"`
	Method           string         `json:"method" binding:"omitempty,oneof=GET HEAD POST PUT PATCH DELETE OPTIONS"`
	ExpectedStatus   int            `json:"expectedStatus" binding:"omitempty,min=100,max=599"`
	ExpectedKeyword  string         `json:"expectedKeyword" binding:"omitempty,max=512"`
	TimeoutSeconds   int            `json:"timeoutSeconds" binding:"omitempty,min=1,max=300"`
	IntervalSeconds  int            `json:"intervalSeconds" binding:"omitempty,min=10,max=86400"`
	FailureThreshold int            `json:"failureThreshold" binding:"omitempty,min=1,max=100"`
	Enabled          bool           `json:"enabled"`
	Status           CheckStatus    `json:"status"`
	ServiceID        string         `json:"serviceId,omitempty"`
	Tags             []string       `json:"tags,omitempty"`
	Config           map[string]any `json:"config,omitempty"`
	// Regions lists the worker regions that should execute this monitor.
	// Empty defaults to ["default"]. RegionConfirmationThreshold sets the
	// minimum number of regions that must agree on a failure before an
	// incident opens — kills false positives from a single flaky vantage.
	Regions                     []string  `json:"regions,omitempty"`
	RegionConfirmationThreshold int       `json:"regionConfirmationThreshold,omitempty"`
	CreatedAt                   time.Time `json:"createdAt"`
	UpdatedAt                   time.Time `json:"updatedAt"`
}

type CheckResult struct {
	ID                    string         `json:"id"`
	OrganizationID        string         `json:"organizationId"`
	MonitorID             string         `json:"monitorId"`
	Status                CheckStatus    `json:"status"`
	Success               bool           `json:"success"`
	ResponseTimeMS        int64          `json:"responseTimeMs"`
	StatusCode            int            `json:"statusCode"`
	Error                 string         `json:"error,omitempty"`
	CheckedAt             time.Time      `json:"checkedAt"`
	DNSMS                 int64          `json:"dnsMs"`
	TCPConnectMS          int64          `json:"tcpConnectMs"`
	TLSHandshakeMS        int64          `json:"tlsHandshakeMs"`
	TimeToFirstByteMS     int64          `json:"timeToFirstByteMs"`
	TotalMS               int64          `json:"totalMs"`
	ResponseSnippet       string         `json:"responseSnippet,omitempty"`
	ConsecutiveFailures   int            `json:"consecutiveFailures,omitempty"`
	MaintenanceSuppressed bool           `json:"maintenanceSuppressed,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
	// DomainExpiresAt is populated by the Domain checker only. Nil for
	// every other check type.
	DomainExpiresAt *time.Time `json:"domainExpiresAt,omitempty"`
	// Region records which worker vantage produced this result. Defaults
	// to "default" for single-region deployments.
	Region string `json:"region,omitempty"`
}

type Incident struct {
	ID                   string           `json:"id"`
	OrganizationID       string           `json:"organizationId"`
	MonitorID            string           `json:"monitorId"`
	Status               IncidentStatus   `json:"status"`
	Severity             IncidentSeverity `json:"severity"`
	Impact               IncidentImpact   `json:"impact"`
	StartedAt            time.Time        `json:"startedAt"`
	ResolvedAt           *time.Time       `json:"resolvedAt,omitempty"`
	AcknowledgedAt       *time.Time       `json:"acknowledgedAt,omitempty"`
	AcknowledgedByUserID string           `json:"acknowledgedByUserId,omitempty"`
	AssignedToUserID     string           `json:"assignedToUserId,omitempty"`
	ResolvedByUserID     string           `json:"resolvedByUserId,omitempty"`
	GroupKey             string           `json:"groupKey,omitempty"`
	ErrorClass           string           `json:"errorClass,omitempty"`
	Flapping             bool             `json:"flapping,omitempty"`
	Suppressed           bool             `json:"suppressed,omitempty"`
	SuppressionReason    string           `json:"suppressionReason,omitempty"`
	Reason               string           `json:"reason"`
	LastError            string           `json:"lastError,omitempty"`
	ConsecutiveFailures  int              `json:"consecutiveFailures"`
	CreatedAt            time.Time        `json:"createdAt"`
	UpdatedAt            time.Time        `json:"updatedAt"`
}

type IncidentTransition struct {
	Status           IncidentStatus   `json:"status" binding:"required,oneof=open acknowledged investigating identified monitoring resolved"`
	Severity         IncidentSeverity `json:"severity,omitempty" binding:"omitempty,oneof=info warning minor major critical"`
	Impact           IncidentImpact   `json:"impact,omitempty" binding:"omitempty,oneof=none degraded partial_outage full_outage"`
	AssignedToUserID string           `json:"assignedToUserId,omitempty"`
	Message          string           `json:"message,omitempty"`
}

type IncidentTimelineEvent struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organizationId"`
	IncidentID     string         `json:"incidentId"`
	EventType      string         `json:"eventType"`
	ActorUserID    string         `json:"actorUserId,omitempty"`
	Message        string         `json:"message,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Evidence       map[string]any `json:"evidence,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type IncidentComment struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	IncidentID     string    `json:"incidentId"`
	AuthorUserID   string    `json:"authorUserId,omitempty"`
	Body           string    `json:"body" binding:"required,min=1"`
	Visibility     string    `json:"visibility" binding:"omitempty,oneof=internal public"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type IncidentPostmortem struct {
	ID                  string    `json:"id"`
	OrganizationID      string    `json:"organizationId"`
	IncidentID          string    `json:"incidentId"`
	Summary             string    `json:"summary,omitempty"`
	RootCause           string    `json:"rootCause,omitempty"`
	Impact              string    `json:"impact,omitempty"`
	Timeline            string    `json:"timeline,omitempty"`
	ContributingFactors []string  `json:"contributingFactors,omitempty"`
	CreatedByUserID     string    `json:"createdByUserId,omitempty"`
	UpdatedByUserID     string    `json:"updatedByUserId,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type IncidentActionItem struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organizationId"`
	IncidentID        string     `json:"incidentId"`
	PostmortemID      string     `json:"postmortemId,omitempty"`
	Title             string     `json:"title" binding:"required,min=1"`
	Description       string     `json:"description,omitempty"`
	OwnerUserID       string     `json:"ownerUserId,omitempty"`
	DueAt             *time.Time `json:"dueAt,omitempty"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	CompletedByUserID string     `json:"completedByUserId,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type IncidentSuppression struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	MonitorID      string    `json:"monitorId"`
	Reason         string    `json:"reason"`
	Details        string    `json:"details,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type MonitorDependency struct {
	ID                 string    `json:"id"`
	OrganizationID     string    `json:"organizationId"`
	MonitorID          string    `json:"monitorId"`
	DependsOnMonitorID string    `json:"dependsOnMonitorId"`
	CreatedAt          time.Time `json:"createdAt"`
}

type NotificationChannel struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	Name           string `json:"name" binding:"required,min=1,max=120"`
	Type           string `json:"type" binding:"required,oneof=webhook slack email smtp pagerduty push discord teams telegram google_chat twilio_sms twilio_voice aws_sns_sms"`
	// URL is kept for backwards-compat with webhook channels; new
	// integrations carry their config in Config jsonb.
	URL       string         `json:"url,omitempty" binding:"omitempty,url,max=2048"`
	Config    map[string]any `json:"config,omitempty"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// PushDevice mirrors a mobile-app installation that wants incident
// notifications. expo_token is the Expo Push token issued client-side
// (Expo abstracts APNs/FCM); we don't ever see the underlying APNs key.
type PushDevice struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	UserID         string    `json:"userId"`
	Platform       string    `json:"platform"`
	ExpoToken      string    `json:"expoToken"`
	AppVersion     string    `json:"appVersion,omitempty"`
	LastSeenAt     time.Time `json:"lastSeenAt"`
	CreatedAt      time.Time `json:"createdAt"`
}

// OutboxEntry is a single pending or completed notification delivery.
// Written inside the same tx as the incident state change so a crash
// between commit and dispatch can't drop the alert.
type OutboxEntry struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	ChannelID      string    `json:"channelId,omitempty"`
	IncidentID     string    `json:"incidentId,omitempty"`
	EventType      string    `json:"eventType"`
	Payload        []byte    `json:"-"`
	Attempts       int       `json:"attempts"`
	NextAttemptAt  time.Time `json:"nextAttemptAt"`
	Status         string    `json:"status"`
	LastError      string    `json:"lastError,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type APIKey struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	Name           string     `json:"name"`
	KeyHash        string     `json:"-"`
	CreatedAt      time.Time  `json:"createdAt"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
}

type OverviewStats struct {
	TotalMonitors       int     `json:"totalMonitors"`
	MonitorsUp          int     `json:"monitorsUp"`
	MonitorsDown        int     `json:"monitorsDown"`
	MonitorsDegraded    int     `json:"monitorsDegraded"`
	OpenIncidents       int     `json:"openIncidents"`
	UptimePercentage24H float64 `json:"uptimePercentage24h"`
	AverageResponseMS   float64 `json:"averageResponseMs"`
	P95ResponseMS       float64 `json:"p95ResponseMs"`
}

type ResultFilter struct {
	MonitorID          string
	ServiceID          string
	StatusPageID       string
	Status             string
	CheckedAfter       *time.Time
	CheckedBefore      *time.Time
	ExcludeMaintenance bool
	Limit              int
	Offset             int
}

type MonitorFilter struct {
	Tag       string
	ServiceID string
	Type      string
	Status    string
	Enabled   *bool
}

type Service struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name" binding:"required,min=1,max=120"`
	Slug           string    `json:"slug" binding:"required,min=1,max=120"`
	Description    string    `json:"description,omitempty" binding:"omitempty,max=2048"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Organization represents a Clerk Organization mirrored locally so we can
// reference org_id from FKs and enforce role checks without hitting Clerk
// on every request. ClerkOrgID is the foreign identifier from Clerk and may
// be empty for the seeded default org or any org created administratively.
type Organization struct {
	ID         string    `json:"id"`
	ClerkOrgID string    `json:"clerkOrgId,omitempty"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	Plan       string    `json:"plan"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// User mirrors a Clerk user record. The local id is a uuid we own; the
// ClerkUserID maps back to the source of truth for profile data.
type User struct {
	ID          string    `json:"id"`
	ClerkUserID string    `json:"clerkUserId,omitempty"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	ImageURL    string    `json:"imageUrl,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Membership records the relationship between a User and an Organization,
// along with their role within that org. A user in N orgs has N membership
// rows.
type Membership struct {
	OrganizationID string    `json:"organizationId"`
	UserID         string    `json:"userId"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// MembershipDetail is a denormalized view returned by list endpoints —
// includes the org metadata so the mobile app can render an org list in a
// single round-trip.
type MembershipDetail struct {
	OrganizationID   string `json:"organizationId"`
	OrganizationName string `json:"organizationName"`
	OrganizationSlug string `json:"organizationSlug"`
	Plan             string `json:"plan"`
	Role             string `json:"role"`
}

// Heartbeat is the per-monitor configuration for push-style (cron) checks.
// Clients POST to /api/v1/heartbeats/:token/ping at their cadence; if a
// ping doesn't arrive within ExpectedIntervalSeconds + GraceSeconds the
// monitor flips to down on the next scheduler tick.
type Heartbeat struct {
	MonitorID               string     `json:"monitorId"`
	TokenHash               string     `json:"-"`
	ExpectedIntervalSeconds int        `json:"expectedIntervalSeconds"`
	GraceSeconds            int        `json:"graceSeconds"`
	LastPingAt              *time.Time `json:"lastPingAt,omitempty"`
	LastPingSourceIP        string     `json:"lastPingSourceIp,omitempty"`
	LastPingUserAgent       string     `json:"lastPingUserAgent,omitempty"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

// MultistepScript holds the JSON DSL describing a sequence of HTTP
// requests with assertions and variable extraction. See
// internal/checks/multistep.go for the semantic.
type MultistepScript struct {
	MonitorID string         `json:"monitorId"`
	Steps     MultistepSteps `json:"steps"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// MultistepSteps is the parsed payload of MultistepScript.Steps. Stored
// as jsonb in Postgres but materialized to typed values for the checker.
type MultistepSteps struct {
	Steps []MultistepStep   `json:"steps"`
	Vars  map[string]string `json:"vars,omitempty"`
}

type MultistepStep struct {
	Name       string               `json:"name,omitempty"`
	Method     string               `json:"method"`
	URL        string               `json:"url"`
	Headers    map[string]string    `json:"headers,omitempty"`
	Body       string               `json:"body,omitempty"`
	Assertions []MultistepAssertion `json:"assert,omitempty"`
	Extract    map[string]string    `json:"extract,omitempty"` // varName → jsonpath
}

type MultistepAssertion struct {
	Status   int    `json:"status,omitempty"`
	JSONPath string `json:"jsonpath,omitempty"`
	Equals   any    `json:"equals,omitempty"`
	Exists   *bool  `json:"exists,omitempty"`
	Contains string `json:"contains,omitempty"`
}

// BrowserScript is the user-supplied Playwright code executed by the
// browser-worker sidecar. The Go API stores and serves it but the actual
// execution happens out-of-process in Node.
type BrowserScript struct {
	MonitorID      string         `json:"monitorId"`
	Source         string         `json:"source"`
	TimeoutSeconds int            `json:"timeoutSeconds,omitempty"`
	Retries        int            `json:"retries,omitempty"`
	Env            map[string]any `json:"env,omitempty"`
	RetentionDays  int            `json:"retentionDays,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// StatusPage is the public-facing status surface for one organization.
// Multiple status pages per org are allowed (e.g. customer-facing vs
// internal). slug serves the hosted /s/:slug; custom_domain is used by
// the host-header middleware when a CNAME points at the API.
type StatusPage struct {
	ID                   string         `json:"id"`
	OrganizationID       string         `json:"organizationId"`
	Slug                 string         `json:"slug"`
	Name                 string         `json:"name"`
	Description          string         `json:"description,omitempty"`
	CustomDomain         string         `json:"customDomain,omitempty"`
	CustomDomainVerified bool           `json:"customDomainVerified"`
	Theme                map[string]any `json:"theme,omitempty"`
	Published            bool           `json:"published"`
	AutoUpdates          bool           `json:"autoUpdates"`
	LogoURL              string         `json:"logoUrl,omitempty"`
	PrimaryColor         string         `json:"primaryColor,omitempty"`
	Public               bool           `json:"public"`
	NoIndex              bool           `json:"noIndex"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

type StatusPageSubscriber struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	StatusPageID   string     `json:"statusPageId"`
	Email          string     `json:"email" binding:"required,email"`
	ConfirmedAt    *time.Time `json:"confirmedAt,omitempty"`
	ComponentIDs   []string   `json:"componentIds,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type StatusPageAnnouncement struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	StatusPageID   string     `json:"statusPageId"`
	Type           string     `json:"type" binding:"omitempty,oneof=general maintenance incident"`
	Title          string     `json:"title" binding:"required,min=1"`
	Body           string     `json:"body,omitempty"`
	Status         string     `json:"status" binding:"omitempty,oneof=draft published archived"`
	IncidentID     string     `json:"incidentId,omitempty"`
	ComponentIDs   []string   `json:"componentIds,omitempty"`
	PublishedAt    *time.Time `json:"publishedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// StatusPageComponent groups monitors into a single visual unit on the
// page (e.g. "API", "Web App"). The component's status is the worst
// status across its constituent monitors.
type StatusPageComponent struct {
	ID           string      `json:"id"`
	StatusPageID string      `json:"statusPageId"`
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	Position     int         `json:"position"`
	MonitorIDs   []string    `json:"monitorIds"`
	GroupName    string      `json:"groupName,omitempty"`
	ServiceID    string      `json:"serviceId,omitempty"`
	ManualStatus CheckStatus `json:"manualStatus,omitempty"`
	OrderIndex   int         `json:"orderIndex"`
	Status       CheckStatus `json:"status,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

// MaintenanceWindow declares a span when checks should be suppressed.
// recurrence_rrule is an RFC 5545 RRULE string for repeating windows
// (e.g. "FREQ=WEEKLY;BYDAY=TU"). status_page_id, when set, surfaces a
// banner on the named status page during the window.
type MaintenanceWindow struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	StartsAt        time.Time `json:"startsAt"`
	EndsAt          time.Time `json:"endsAt"`
	Timezone        string    `json:"timezone,omitempty"`
	Recurrence      string    `json:"recurrence,omitempty"`
	RecurrenceRRule string    `json:"recurrenceRrule,omitempty"`
	StatusPageID    string    `json:"statusPageId,omitempty"`
	CreatedByUserID string    `json:"createdByUserId,omitempty"`
	Enabled         bool      `json:"enabled"`
	MonitorIDs      []string  `json:"monitorIds"`
	TagNames        []string  `json:"tagNames,omitempty"`
	Active          bool      `json:"active,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Tag is a user-defined label that can be attached to monitors. Tag
// names are unique per organization. The color is purely cosmetic; the
// UI uses it for the pill background.
type Tag struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name" binding:"required,min=1,max=64"`
	Color          string    `json:"color" binding:"omitempty,len=7,startswith=#"`
	CreatedAt      time.Time `json:"createdAt"`
}

type PublicStatusPage struct {
	Page            StatusPage            `json:"page"`
	Status          CheckStatus           `json:"status"`
	Components      []StatusPageComponent `json:"components"`
	ActiveIncidents []Incident            `json:"activeIncidents"`
	RecentIncidents []Incident            `json:"recentIncidents"`
	Uptime24H       float64               `json:"uptime24h"`
	GeneratedAt     time.Time             `json:"generatedAt"`
}

type HeartbeatEvent struct {
	ID         string         `json:"id"`
	MonitorID  string         `json:"monitorId"`
	Status     CheckStatus    `json:"status,omitempty"`
	Message    string         `json:"message,omitempty"`
	DurationMS int64          `json:"durationMs,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type UptimeReport struct {
	MonitorID         string    `json:"monitorId,omitempty"`
	ServiceID         string    `json:"serviceId,omitempty"`
	StatusPageID      string    `json:"statusPageId,omitempty"`
	From              time.Time `json:"from"`
	To                time.Time `json:"to"`
	Checks            int       `json:"checks"`
	SuccessfulChecks  int       `json:"successfulChecks"`
	UptimePercentage  float64   `json:"uptimePercentage"`
	DowntimeMinutes   float64   `json:"downtimeMinutes"`
	AverageResponseMS float64   `json:"averageResponseMs"`
	P50ResponseMS     float64   `json:"p50ResponseMs"`
	P95ResponseMS     float64   `json:"p95ResponseMs"`
	P99ResponseMS     float64   `json:"p99ResponseMs"`
	IncidentCount     int       `json:"incidentCount"`
	GeneratedAt       time.Time `json:"generatedAt"`
}

type UptimeReportFilter struct {
	MonitorID          string
	ServiceID          string
	StatusPageID       string
	From               time.Time
	To                 time.Time
	ExcludeMaintenance bool
}

// SLAReport is the response shape of the /api/v1/sla/* endpoints.
// MaintenanceSeconds is subtracted from RawDownSeconds to produce
// BillableDownSeconds, which is what most SLA contracts measure.
type SLAReport struct {
	MonitorID           string    `json:"monitorId,omitempty"`
	Period              string    `json:"period"`
	From                time.Time `json:"from"`
	To                  time.Time `json:"to"`
	UptimePercentage    float64   `json:"uptimePercentage"`
	RawDownSeconds      int64     `json:"rawDownSeconds"`
	MaintenanceSeconds  int64     `json:"maintenanceSeconds"`
	BillableDownSeconds int64     `json:"billableDownSeconds"`
	IncidentCount       int       `json:"incidents"`
}

// WorkerHeartbeat is the periodic liveness + state snapshot a worker
// process writes to the database so the API can surface a "what's running
// right now" view without sharing memory across processes.
//
// InFlight is the set of monitor IDs currently being checked at the moment
// the heartbeat was written; ordering is not stable. JobsCompleted and
// JobsFailed are monotonic counters scoped to the worker's lifetime — they
// reset when the process restarts.
type WorkerHeartbeat struct {
	InstanceID    string    `json:"instanceId"`
	Hostname      string    `json:"hostname"`
	Version       string    `json:"version"`
	Region        string    `json:"region,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
	WorkerCount   int       `json:"workerCount"`
	ActiveJobs    int       `json:"activeJobs"`
	QueueDepth    int       `json:"queueDepth"`
	QueueCapacity int       `json:"queueCapacity"`
	JobsCompleted int64     `json:"jobsCompleted"`
	JobsFailed    int64     `json:"jobsFailed"`
	InFlight      []string  `json:"inFlight"`
	// Stale is set by the API when LastSeenAt is older than the freshness
	// window. Workers never write to it.
	Stale bool `json:"stale,omitempty"`
}

type Agent struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	Name           string     `json:"name" binding:"required,min=1,max=120"`
	Region         string     `json:"region" binding:"required,min=1,max=80"`
	TokenHash      string     `json:"-"`
	LastSeenAt     *time.Time `json:"lastSeenAt,omitempty"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type AgentJob struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agentId"`
	Monitor   Monitor   `json:"monitor"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type OnCallSchedule struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	Name            string    `json:"name" binding:"required,min=1,max=120"`
	Timezone        string    `json:"timezone" binding:"required"`
	Participants    []string  `json:"participants" binding:"required"`
	RotationSeconds int       `json:"rotationSeconds" binding:"required,min=60"`
	HandoffAt       time.Time `json:"handoffAt"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type OnCallOverride struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	ScheduleID     string    `json:"scheduleId"`
	UserID         string    `json:"userId" binding:"required"`
	StartsAt       time.Time `json:"startsAt"`
	EndsAt         time.Time `json:"endsAt"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type OnCallShift struct {
	ScheduleID string    `json:"scheduleId"`
	UserID     string    `json:"userId"`
	StartsAt   time.Time `json:"startsAt"`
	EndsAt     time.Time `json:"endsAt"`
	Override   bool      `json:"override,omitempty"`
}

type EscalationStep struct {
	DelaySeconds int      `json:"delaySeconds"`
	ChannelIDs   []string `json:"channelIds,omitempty"`
	ScheduleID   string   `json:"scheduleId,omitempty"`
	TargetUserID string   `json:"targetUserId,omitempty"`
	Method       string   `json:"method,omitempty"`
}

type EscalationPolicy struct {
	ID             string           `json:"id"`
	OrganizationID string           `json:"organizationId"`
	Name           string           `json:"name" binding:"required,min=1,max=120"`
	Enabled        bool             `json:"enabled"`
	ServiceID      string           `json:"serviceId,omitempty"`
	MonitorID      string           `json:"monitorId,omitempty"`
	TagName        string           `json:"tagName,omitempty"`
	Severity       IncidentSeverity `json:"severity,omitempty"`
	Impact         IncidentImpact   `json:"impact,omitempty"`
	Steps          []EscalationStep `json:"steps"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

type Runbook struct {
	ID             string           `json:"id"`
	OrganizationID string           `json:"organizationId"`
	Title          string           `json:"title" binding:"required,min=1,max=160"`
	URL            string           `json:"url,omitempty"`
	Content        string           `json:"content,omitempty"`
	MonitorID      string           `json:"monitorId,omitempty"`
	ServiceID      string           `json:"serviceId,omitempty"`
	TagName        string           `json:"tagName,omitempty"`
	Severity       IncidentSeverity `json:"severity,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

type BrowserArtifact struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organizationId"`
	MonitorID      string         `json:"monitorId"`
	CheckResultID  string         `json:"checkResultId,omitempty"`
	Type           string         `json:"type"`
	Path           string         `json:"path,omitempty"`
	Public         bool           `json:"public"`
	SizeBytes      int64          `json:"sizeBytes,omitempty"`
	ExpiresAt      *time.Time     `json:"expiresAt,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
}
