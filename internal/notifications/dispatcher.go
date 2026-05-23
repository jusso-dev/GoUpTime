// Dispatcher routes Events to the right Provider based on
// NotificationChannel.Type. It is the seam between the incident-rule
// path (service/monitoring.go) and the durable outbox.

package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
)

type Dispatcher struct {
	store     repository.Store
	providers map[string]Provider
	logger    *slog.Logger
}

func NewDispatcher(store repository.Store, logger *slog.Logger, providers ...Provider) *Dispatcher {
	m := map[string]Provider{}
	for _, p := range providers {
		m[p.Type()] = p
	}
	return &Dispatcher{store: store, providers: m, logger: logger}
}

// Enqueue writes an outbox row for every enabled channel in the org.
// The actual HTTP work happens in the poller (outbox.go) so callers
// return immediately and survive a crash before delivery.
func (d *Dispatcher) Enqueue(ctx context.Context, orgID string, incidentID string, event Event) error {
	if orgID == "" {
		return fmt.Errorf("dispatcher: organization id required")
	}
	sysCtx := auth.WithSystemOrg(ctx, orgID)
	channels, err := d.store.ListNotificationChannels(sysCtx)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}
	payload, _ := json.Marshal(event)
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if _, ok := d.providers[ch.Type]; !ok {
			// Unknown channel type — skip without erroring; lets us
			// roll new types out in stages.
			d.logger.Warn("dispatcher: no provider for channel type",
				"channel_id", ch.ID, "type", ch.Type)
			continue
		}
		if _, err := d.store.EnqueueNotification(sysCtx, models.OutboxEntry{
			OrganizationID: orgID,
			ChannelID:      ch.ID,
			IncidentID:     incidentID,
			EventType:      event.Type,
			Payload:        payload,
		}); err != nil {
			d.logger.Error("dispatcher: enqueue failed",
				"channel_id", ch.ID, "error", err)
		}
	}
	return nil
}

// ProviderFor returns the registered provider for a channel type, or
// false if none. The poller uses this when claiming and dispatching
// outbox entries.
func (d *Dispatcher) ProviderFor(channelType string) (Provider, bool) {
	p, ok := d.providers[channelType]
	return p, ok
}

// Store exposes the underlying store for the poller.
func (d *Dispatcher) Store() repository.Store { return d.store }
