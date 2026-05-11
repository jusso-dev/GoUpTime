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

type Monitor struct {
	ID               string      `json:"id"`
	Name             string      `json:"name" binding:"required"`
	Type             MonitorType `json:"type" binding:"required,oneof=http tcp dns tls keyword"`
	Target           string      `json:"target" binding:"required"`
	Method           string      `json:"method"`
	ExpectedStatus   int         `json:"expectedStatus"`
	ExpectedKeyword  string      `json:"expectedKeyword"`
	TimeoutSeconds   int         `json:"timeoutSeconds"`
	IntervalSeconds  int         `json:"intervalSeconds"`
	FailureThreshold int         `json:"failureThreshold"`
	Enabled          bool        `json:"enabled"`
	Status           CheckStatus `json:"status"`
	CreatedAt        time.Time   `json:"createdAt"`
	UpdatedAt        time.Time   `json:"updatedAt"`
}

type CheckResult struct {
	ID                  string      `json:"id"`
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
	ID                  string         `json:"id"`
	MonitorID           string         `json:"monitorId"`
	Status              IncidentStatus `json:"status"`
	StartedAt           time.Time      `json:"startedAt"`
	ResolvedAt          *time.Time     `json:"resolvedAt,omitempty"`
	Reason              string         `json:"reason"`
	LastError           string         `json:"lastError,omitempty"`
	ConsecutiveFailures int            `json:"consecutiveFailures"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type NotificationChannel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name" binding:"required"`
	Type      string    `json:"type" binding:"required,oneof=webhook"`
	URL       string    `json:"url,omitempty" binding:"required"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type APIKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
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
