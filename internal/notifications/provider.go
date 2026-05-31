// Provider is the interface every notification destination implements.
// New integrations (Discord, Teams, Opsgenie, …) drop in by adding a
// Provider implementation and registering it with the Dispatcher; the
// rest of the codebase remains unaware.

package notifications

import (
	"context"

	"github.com/jusso-dev/uptime/internal/models"
)

// Event is the canonical incident-lifecycle payload that every Provider
// receives. Provider implementations translate it into their own wire
// shape (Slack Block Kit, Expo push body, etc.).
type Event struct {
	Type        string         // incident.opened | incident.resolved | incident.acknowledged
	IncidentID  string
	MonitorID   string
	MonitorName string
	Status      string
	Reason      string
	StartedAt   string
	ResolvedAt  string
	Region      string
	URL         string // deep link to the incident in the dashboard
}

// Delivery is what Provider.Send returns: the HTTP status code (or 0 for
// non-HTTP integrations) and a Retryable flag so the dispatcher knows
// whether to schedule another attempt.
type Delivery struct {
	StatusCode int
	Retryable  bool
}

// Provider knows how to translate an Event into a delivery for one
// channel. Implementations are stateless and safe for concurrent use.
type Provider interface {
	Type() string
	Send(ctx context.Context, channel models.NotificationChannel, event Event) (Delivery, error)
}
