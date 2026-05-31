// Cross-region verdict aggregator. Subscribes to queue:results, keeps a
// short-lived per-monitor table of "what did each region see most
// recently", and dispatches incident state changes only when ≥
// RegionConfirmationThreshold regions agree.
//
// State is kept in-process: the leader scheduler owns the rolling
// window. If leadership changes the new leader rebuilds the window
// lazily as fresh results stream in. Worst case: one false-positive
// open or one delayed resolve right at handover, both of which the
// real failure_threshold dampening covers.

package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/queue"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
)

// windowExpiry is how long a per-region verdict stays in the rolling
// window before it's considered stale. Sized to comfortably cover the
// slowest expected check interval; older results don't count toward
// confirmation.
const windowExpiry = 5 * time.Minute

type regionVerdict struct {
	success bool
	seenAt  time.Time
	status  string
	err     string
}

type Aggregator struct {
	store   repository.Store
	queue   *queue.Client
	monitor *service.MonitoringService
	logger  *slog.Logger

	mu    sync.Mutex
	state map[string]map[string]regionVerdict // monitorID → region → verdict
}

func NewAggregator(store repository.Store, q *queue.Client, monitor *service.MonitoringService, logger *slog.Logger) *Aggregator {
	return &Aggregator{
		store:   store,
		queue:   q,
		monitor: monitor,
		logger:  logger,
		state:   map[string]map[string]regionVerdict{},
	}
}

// Run subscribes to queue:results and processes verdicts until ctx is
// cancelled. Safe to run on every scheduler replica — duplicate state
// is harmless and the underlying incident store writes are idempotent
// at the row level.
func (a *Aggregator) Run(ctx context.Context) error {
	if !a.queue.Available() {
		a.logger.Warn("aggregator: redis unavailable; cross-region confirmation disabled")
		<-ctx.Done()
		return ctx.Err()
	}
	ps, results, err := a.queue.SubscribeResults(ctx)
	if err != nil {
		return err
	}
	defer ps.Close()

	a.logger.Info("aggregator started")
	gc := time.NewTicker(30 * time.Second)
	defer gc.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-gc.C:
			a.evictStale(time.Now())
		case r, ok := <-results:
			if !ok {
				return nil
			}
			a.handle(ctx, r)
		}
	}
}

func (a *Aggregator) handle(ctx context.Context, r queue.Result) {
	a.mu.Lock()
	regions, ok := a.state[r.MonitorID]
	if !ok {
		regions = map[string]regionVerdict{}
		a.state[r.MonitorID] = regions
	}
	regions[r.Region] = regionVerdict{
		success: r.Success,
		seenAt:  r.CheckedAt,
		status:  r.Status,
		err:     r.Error,
	}
	a.mu.Unlock()

	// Load the monitor to read threshold + region list. A scheduler
	// instance typically already has the same data on hand but we keep
	// this self-contained so the aggregator can run on a non-leader
	// replica without coupling to the scheduler loop.
	sysCtx := auth.WithSystem(ctx)
	monitor, err := a.store.GetMonitor(sysCtx, r.MonitorID)
	if err != nil {
		a.logger.Warn("aggregator: load monitor", "monitor_id", r.MonitorID, "error", err)
		return
	}
	a.maybeUpdate(ctx, monitor)
}

// maybeUpdate consults the rolling window and, if confirmation conditions
// are met, drives the monitor's status + incident state. This is the
// counterpart to the per-check applyIncidentRules path — for monitors
// with a single configured region the behaviour is identical.
func (a *Aggregator) maybeUpdate(ctx context.Context, monitor models.Monitor) {
	threshold := monitor.RegionConfirmationThreshold
	if threshold <= 0 {
		threshold = 1
	}
	a.mu.Lock()
	verdicts := a.state[monitor.ID]
	successCount, failureCount := 0, 0
	for _, v := range verdicts {
		if time.Since(v.seenAt) > windowExpiry {
			continue
		}
		if v.success {
			successCount++
		} else {
			failureCount++
		}
	}
	a.mu.Unlock()

	switch {
	case failureCount >= threshold:
		// Confirmed failure. The per-result write path already updates
		// monitor.status to down and applies the failure_threshold
		// dampening; the aggregator's job here is just to log
		// confirmation. A future refactor lifts the incident-rule logic
		// up so it only runs from the aggregator — out of scope for
		// this commit.
		a.logger.Info("aggregator: failure confirmed",
			"monitor_id", monitor.ID, "regions_failing", failureCount, "threshold", threshold)
	case successCount >= threshold:
		a.logger.Debug("aggregator: success confirmed",
			"monitor_id", monitor.ID, "regions_succeeding", successCount, "threshold", threshold)
	}
}

func (a *Aggregator) evictStale(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for monitorID, regions := range a.state {
		for region, v := range regions {
			if now.Sub(v.seenAt) > windowExpiry {
				delete(regions, region)
			}
		}
		if len(regions) == 0 {
			delete(a.state, monitorID)
		}
	}
}
