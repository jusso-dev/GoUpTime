package models

import "time"

type MonitorType string
type CheckStatus string
type IncidentStatus string

const (
	MonitorHTTP      MonitorType = "http"
	MonitorTCP       MonitorType = "tcp"
	MonitorDNS       MonitorType = "dns"
	MonitorTLS       MonitorType = "tls"
	MonitorKeyword   MonitorType = "keyword"
	MonitorHeartbeat MonitorType = "heartbeat"
	MonitorICMP      MonitorType = "icmp"
	MonitorBrowser   MonitorType = "browser"
	MonitorDomain    MonitorType = "domain"
	MonitorMultistep MonitorType = "multistep"

	StatusUp       CheckStatus = "up"
	StatusDown     CheckStatus = "down"
	StatusDegraded CheckStatus = "degraded"

	IncidentOpen     IncidentStatus = "open"
	IncidentResolved IncidentStatus = "resolved"
)

// Monitor is the canonical synthetic-check definition. Status, CreatedAt,
// and UpdatedAt are server-managed; clients can submit them but the API
// layer zeroes Status on create so users cannot manufacture "up" state.
// OrganizationID is filled in by the repository from the request principal
// — clients cannot set it on create or update.
type Monitor struct {
	ID               string      `json:"id"`
	OrganizationID   string      `json:"organizationId"`
	Name             string      `json:"name" binding:"required,min=1,max=120"`
	Type             MonitorType `json:"type" binding:"required,oneof=http tcp dns tls keyword heartbeat icmp browser domain multistep"`
	// Target is required for network-based checks (HTTP/TCP/DNS/TLS/ICMP/
	// Domain). Heartbeat, Browser, and Multistep monitors store their
	// configuration in dedicated tables; the binding tag is intentionally
	// loose here and the API handler enforces per-type rules.
	Target           string      `json:"target" binding:"omitempty,max=2048"`
	Method           string      `json:"method" binding:"omitempty,oneof=GET HEAD"`
	ExpectedStatus   int         `json:"expectedStatus" binding:"omitempty,min=100,max=599"`
	ExpectedKeyword  string      `json:"expectedKeyword" binding:"omitempty,max=512"`
	TimeoutSeconds   int         `json:"timeoutSeconds" binding:"omitempty,min=1,max=300"`
	IntervalSeconds  int         `json:"intervalSeconds" binding:"omitempty,min=10,max=86400"`
	FailureThreshold int         `json:"failureThreshold" binding:"omitempty,min=1,max=100"`
	Enabled          bool        `json:"enabled"`
	Status           CheckStatus `json:"status"`
	// Regions lists the worker regions that should execute this monitor.
	// Empty defaults to ["default"]. RegionConfirmationThreshold sets the
	// minimum number of regions that must agree on a failure before an
	// incident opens — kills false positives from a single flaky vantage.
	Regions                      []string `json:"regions,omitempty"`
	RegionConfirmationThreshold  int      `json:"regionConfirmationThreshold,omitempty"`
	CreatedAt                    time.Time `json:"createdAt"`
	UpdatedAt                    time.Time `json:"updatedAt"`
}

type CheckResult struct {
	ID                  string      `json:"id"`
	OrganizationID      string      `json:"organizationId"`
	MonitorID           string      `json:"monitorId"`
	Status              CheckStatus `json:"status"`
	Success             bool        `json:"success"`
	ResponseTimeMS      int64       `json:"responseTimeMs"`
	StatusCode          int         `json:"statusCode"`
	Error               string      `json:"error,omitempty"`
	CheckedAt           time.Time   `json:"checkedAt"`
	DNSMS               int64       `json:"dnsMs"`
	TCPConnectMS        int64       `json:"tcpConnectMs"`
	TLSHandshakeMS      int64       `json:"tlsHandshakeMs"`
	TimeToFirstByteMS   int64       `json:"timeToFirstByteMs"`
	TotalMS             int64       `json:"totalMs"`
	ResponseSnippet     string      `json:"responseSnippet,omitempty"`
	ConsecutiveFailures int         `json:"consecutiveFailures,omitempty"`
	// DomainExpiresAt is populated by the Domain checker only. Nil for
	// every other check type.
	DomainExpiresAt *time.Time `json:"domainExpiresAt,omitempty"`
	// Region records which worker vantage produced this result. Defaults
	// to "default" for single-region deployments.
	Region string `json:"region,omitempty"`
}

type Incident struct {
	ID                   string         `json:"id"`
	OrganizationID       string         `json:"organizationId"`
	MonitorID            string         `json:"monitorId"`
	Status               IncidentStatus `json:"status"`
	StartedAt            time.Time      `json:"startedAt"`
	ResolvedAt           *time.Time     `json:"resolvedAt,omitempty"`
	AcknowledgedAt       *time.Time     `json:"acknowledgedAt,omitempty"`
	AcknowledgedByUserID string         `json:"acknowledgedByUserId,omitempty"`
	Reason               string         `json:"reason"`
	LastError            string         `json:"lastError,omitempty"`
	ConsecutiveFailures  int            `json:"consecutiveFailures"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

type NotificationChannel struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organizationId"`
	Name           string         `json:"name" binding:"required,min=1,max=120"`
	Type           string         `json:"type" binding:"required,oneof=webhook slack email pagerduty push"`
	// URL is kept for backwards-compat with webhook channels; new
	// integrations carry their config in Config jsonb.
	URL       string                 `json:"url,omitempty" binding:"omitempty,url,max=2048"`
	Config    map[string]any         `json:"config,omitempty"`
	Enabled   bool                   `json:"enabled"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
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
	MonitorID     string
	Status        string
	CheckedAfter  *time.Time
	CheckedBefore *time.Time
	Limit         int
	Offset        int
}

// Organization represents a Clerk Organization mirrored locally so we can
// reference org_id from FKs and enforce role checks without hitting Clerk
// on every request. ClerkOrgID is the foreign identifier from Clerk and may
// be empty for the seeded default org or any org created administratively.
type Organization struct {
	ID          string    `json:"id"`
	ClerkOrgID  string    `json:"clerkOrgId,omitempty"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Plan        string    `json:"plan"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
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
	MonitorID                string     `json:"monitorId"`
	TokenHash                string     `json:"-"`
	ExpectedIntervalSeconds  int        `json:"expectedIntervalSeconds"`
	GraceSeconds             int        `json:"graceSeconds"`
	LastPingAt               *time.Time `json:"lastPingAt,omitempty"`
	LastPingSourceIP         string     `json:"lastPingSourceIp,omitempty"`
	LastPingUserAgent        string     `json:"lastPingUserAgent,omitempty"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

// MultistepScript holds the JSON DSL describing a sequence of HTTP
// requests with assertions and variable extraction. See
// internal/checks/multistep.go for the semantic.
type MultistepScript struct {
	MonitorID string          `json:"monitorId"`
	Steps     MultistepSteps  `json:"steps"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// MultistepSteps is the parsed payload of MultistepScript.Steps. Stored
// as jsonb in Postgres but materialized to typed values for the checker.
type MultistepSteps struct {
	Steps []MultistepStep `json:"steps"`
	Vars  map[string]string `json:"vars,omitempty"`
}

type MultistepStep struct {
	Name       string            `json:"name,omitempty"`
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Assertions []MultistepAssertion `json:"assert,omitempty"`
	Extract    map[string]string `json:"extract,omitempty"` // varName → jsonpath
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
	MonitorID string    `json:"monitorId"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

// StatusPageComponent groups monitors into a single visual unit on the
// page (e.g. "API", "Web App"). The component's status is the worst
// status across its constituent monitors.
type StatusPageComponent struct {
	ID           string    `json:"id"`
	StatusPageID string    `json:"statusPageId"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Position     int       `json:"position"`
	MonitorIDs   []string  `json:"monitorIds"`
	GroupName    string    `json:"groupName,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// MaintenanceWindow declares a span when checks should be suppressed.
// recurrence_rrule is an RFC 5545 RRULE string for repeating windows
// (e.g. "FREQ=WEEKLY;BYDAY=TU"). status_page_id, when set, surfaces a
// banner on the named status page during the window.
type MaintenanceWindow struct {
	ID                 string     `json:"id"`
	OrganizationID     string     `json:"organizationId"`
	Name               string     `json:"name"`
	Description        string     `json:"description,omitempty"`
	StartsAt           time.Time  `json:"startsAt"`
	EndsAt             time.Time  `json:"endsAt"`
	RecurrenceRRule    string     `json:"recurrenceRrule,omitempty"`
	StatusPageID       string     `json:"statusPageId,omitempty"`
	CreatedByUserID    string     `json:"createdByUserId,omitempty"`
	MonitorIDs         []string   `json:"monitorIds"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

// Tag is a user-defined label that can be attached to monitors. Tag
// names are unique per organization. The color is purely cosmetic; the
// UI uses it for the pill background.
type Tag struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	Color          string    `json:"color"`
	CreatedAt      time.Time `json:"createdAt"`
}

// SLAReport is the response shape of the /api/v1/sla/* endpoints.
// MaintenanceSeconds is subtracted from RawDownSeconds to produce
// BillableDownSeconds, which is what most SLA contracts measure.
type SLAReport struct {
	MonitorID             string    `json:"monitorId,omitempty"`
	Period                string    `json:"period"`
	From                  time.Time `json:"from"`
	To                    time.Time `json:"to"`
	UptimePercentage      float64   `json:"uptimePercentage"`
	RawDownSeconds        int64     `json:"rawDownSeconds"`
	MaintenanceSeconds    int64     `json:"maintenanceSeconds"`
	BillableDownSeconds   int64     `json:"billableDownSeconds"`
	IncidentCount         int       `json:"incidents"`
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
	InstanceID     string    `json:"instanceId"`
	Hostname       string    `json:"hostname"`
	Version        string    `json:"version"`
	Region         string    `json:"region,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	LastSeenAt     time.Time `json:"lastSeenAt"`
	WorkerCount    int       `json:"workerCount"`
	ActiveJobs     int       `json:"activeJobs"`
	QueueDepth     int       `json:"queueDepth"`
	QueueCapacity  int       `json:"queueCapacity"`
	JobsCompleted  int64     `json:"jobsCompleted"`
	JobsFailed     int64     `json:"jobsFailed"`
	InFlight       []string  `json:"inFlight"`
	// Stale is set by the API when LastSeenAt is older than the freshness
	// window. Workers never write to it.
	Stale bool `json:"stale,omitempty"`
}
