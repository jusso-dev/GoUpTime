package models

import "time"

type MonitorType string
type CheckStatus string
type IncidentStatus string

const (
	MonitorHTTP    MonitorType = "http"
	MonitorTCP     MonitorType = "tcp"
	MonitorDNS     MonitorType = "dns"
	MonitorTLS     MonitorType = "tls"
	MonitorKeyword MonitorType = "keyword"

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
	Type             MonitorType `json:"type" binding:"required,oneof=http tcp dns tls keyword"`
	Target           string      `json:"target" binding:"required,min=1,max=2048"`
	Method           string      `json:"method" binding:"omitempty,oneof=GET HEAD"`
	ExpectedStatus   int         `json:"expectedStatus" binding:"omitempty,min=100,max=599"`
	ExpectedKeyword  string      `json:"expectedKeyword" binding:"omitempty,max=512"`
	TimeoutSeconds   int         `json:"timeoutSeconds" binding:"omitempty,min=1,max=300"`
	IntervalSeconds  int         `json:"intervalSeconds" binding:"omitempty,min=10,max=86400"`
	FailureThreshold int         `json:"failureThreshold" binding:"omitempty,min=1,max=100"`
	Enabled          bool        `json:"enabled"`
	Status           CheckStatus `json:"status"`
	CreatedAt        time.Time   `json:"createdAt"`
	UpdatedAt        time.Time   `json:"updatedAt"`
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
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name" binding:"required,min=1,max=120"`
	Type           string    `json:"type" binding:"required,oneof=webhook"`
	URL            string    `json:"url,omitempty" binding:"required,url,max=2048"`
	Enabled        bool      `json:"enabled"`
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
