// Outbox poller. Drains pending notification_outbox rows, hands each
// to the right Provider, and updates the row to delivered or schedules
// a retry. The repository claim operation is concurrency-safe so several
// poller replicas can run at the same time without stepping on each other.

package notifications

import (
	"context"
	"encoding/json"
	"errors"
	mathrand "math/rand/v2"
	"time"

	"log/slog"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

// maxAttempts caps retries before a row is marked failed. The schedule
// is 1s, 4s, 16s, 1m, 5m, 30m, 1h, 1h — eight retries cover ~36 minutes.
const maxAttempts = 8

// pollInterval is how often the poller looks for new rows. The work
// itself is bounded by the per-loop batch size (50), so most cycles
// finish well under the interval.
const pollInterval = 1 * time.Second

type Poller struct {
	dispatcher *Dispatcher
	logger     *slog.Logger
}

func NewPoller(dispatcher *Dispatcher, logger *slog.Logger) *Poller {
	return &Poller{dispatcher: dispatcher, logger: logger}
}

// Run blocks until ctx is cancelled. Returns ctx.Err() on shutdown.
func (p *Poller) Run(ctx context.Context) error {
	p.logger.Info("notification outbox poller started")
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			p.drainOnce(ctx)
		}
	}
}

func (p *Poller) drainOnce(ctx context.Context) {
	store := p.dispatcher.Store()
	entries, err := store.ClaimPendingNotifications(auth.WithSystem(ctx), 50)
	if err != nil {
		p.logger.Warn("outbox: claim failed", "error", err)
		return
	}
	if len(entries) == 0 {
		return
	}

	for _, entry := range entries {
		p.deliver(ctx, entry)
	}
}

func (p *Poller) deliver(ctx context.Context, entry models.OutboxEntry) {
	store := p.dispatcher.Store()
	sysCtx := auth.WithSystemOrg(ctx, entry.OrganizationID)
	channel, err := store.GetNotificationChannel(sysCtx, entry.ChannelID)
	if err != nil {
		// Channel deleted between enqueue and dispatch; mark delivered
		// (silently swallowed) so we don't loop forever.
		_ = store.MarkNotificationDelivered(ctx, entry.ID)
		return
	}
	provider, ok := p.dispatcher.ProviderFor(channel.Type)
	if !ok {
		_ = store.MarkNotificationDelivered(ctx, entry.ID)
		return
	}

	var event Event
	if err := json.Unmarshal(entry.Payload, &event); err != nil {
		_ = store.MarkNotificationRetry(ctx, entry.ID, entry.Attempts+1, maxAttempts,
			"decode payload: "+err.Error(), time.Now().Add(time.Minute))
		return
	}

	_, sendErr := provider.Send(ctx, channel, event)
	attempts := entry.Attempts + 1
	if sendErr == nil {
		if err := store.MarkNotificationDelivered(ctx, entry.ID); err != nil {
			p.logger.Warn("outbox: mark delivered failed", "id", entry.ID, "error", err)
		}
		return
	}
	if errors.Is(sendErr, context.Canceled) {
		return
	}
	backoff := backoffFor(attempts)
	if err := store.MarkNotificationRetry(ctx, entry.ID, attempts, maxAttempts,
		sendErr.Error(), time.Now().Add(backoff)); err != nil {
		p.logger.Warn("outbox: mark retry failed", "id", entry.ID, "error", err)
	}
}

// backoffFor returns the next-attempt delay for the given attempt count.
// 1s, 4s, 16s, 1m, 5m, 30m, 1h, 1h — with ±20% jitter.
func backoffFor(attempts int) time.Duration {
	base := []time.Duration{
		1 * time.Second,
		4 * time.Second,
		16 * time.Second,
		1 * time.Minute,
		5 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
		1 * time.Hour,
	}
	idx := attempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(base) {
		idx = len(base) - 1
	}
	d := base[idx]
	jitter := time.Duration(float64(d) * 0.2 * (mathrand.Float64() - 0.5) * 2)
	return d + jitter
}
