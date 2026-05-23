// Package scheduler runs the leader-elected loop that decides which
// monitors are due and pushes per-region jobs onto Redis queues. It's
// invoked from cmd/scheduler as a standalone binary; cmd/worker keeps
// its in-process scheduler as a fallback for deployments without Redis.
//
// Architecture (high-level):
//   - One scheduler replica wins the lock and becomes leader; the rest
//     poll the lock and stand by.
//   - Leader iterates enabled monitors every SchedulerTick, deciding
//     which are due based on the in-memory nextRun map.
//   - For each due monitor, the leader pushes one job to each of the
//     monitor's configured regions (queue:checks:{region}).
//   - Workers in each region BRPOP from their queue and execute. The
//     aggregator (this package's Aggregator) subscribes to queue:results
//     and folds verdicts into a per-monitor rolling window, opening
//     incidents only when ≥ RegionConfirmationThreshold regions agree.

package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	mathrand "math/rand/v2"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/queue"
	"github.com/jusso-dev/uptime/internal/repository"
)

// leaderTTL is how long a single SET NX lasts. Standby replicas refresh
// every leaderTTL/3 so a temporary stall doesn't lose the lock.
const leaderTTL = 10 * time.Second

type Scheduler struct {
	cfg    config.Config
	store  repository.Store
	queue  *queue.Client
	logger *slog.Logger

	instanceID string
	hostname   string
	nextRun    map[string]time.Time
}

func New(cfg config.Config, store repository.Store, q *queue.Client, logger *slog.Logger) *Scheduler {
	hostname, _ := os.Hostname()
	return &Scheduler{
		cfg:        cfg,
		store:      store,
		queue:      q,
		logger:     logger,
		instanceID: uuid.NewString(),
		hostname:   hostname,
		nextRun:    map[string]time.Time{},
	}
}

// Run blocks until ctx is cancelled. Returns ctx.Err() on shutdown.
func (s *Scheduler) Run(ctx context.Context) error {
	if !s.queue.Available() {
		// No Redis means the scheduler can't dispatch; fall back is the
		// in-process worker scheduler that ships in worker.go. Log and
		// stay alive so the binary can serve metrics without spamming.
		s.logger.Warn("scheduler: redis unavailable; standing by (no leadership possible)")
		<-ctx.Done()
		return ctx.Err()
	}
	leaderTick := time.NewTicker(leaderTTL / 3)
	defer leaderTick.Stop()
	schedTick := time.NewTicker(s.cfg.SchedulerTick())
	defer schedTick.Stop()

	leader := false

	// Try to acquire leadership immediately so a fresh deploy doesn't
	// wait the first tick before scheduling.
	leader = s.attemptLeader(ctx, leader)
	if leader {
		s.enqueueDue(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			if leader {
				_ = s.queue.ReleaseLeader(context.Background(), s.instanceID)
			}
			return ctx.Err()
		case <-leaderTick.C:
			leader = s.attemptLeader(ctx, leader)
		case <-schedTick.C:
			if leader {
				s.enqueueDue(ctx)
			}
		}
	}
}

func (s *Scheduler) attemptLeader(ctx context.Context, was bool) bool {
	if was {
		ok, err := s.queue.RefreshLeader(ctx, s.instanceID, leaderTTL)
		if err != nil {
			s.logger.Warn("scheduler: refresh leader failed", "error", err)
			return false
		}
		if !ok {
			s.logger.Info("scheduler: lost leadership")
			return false
		}
		return true
	}
	ok, err := s.queue.AcquireLeader(ctx, s.instanceID, leaderTTL)
	if err != nil {
		s.logger.Warn("scheduler: acquire leader failed", "error", err)
		return false
	}
	if ok {
		s.logger.Info("scheduler: acquired leadership", "instance_id", s.instanceID, "hostname", s.hostname)
	}
	return ok
}

func (s *Scheduler) enqueueDue(ctx context.Context) {
	monitors, err := s.store.ListEnabledMonitors(auth.WithSystem(ctx))
	if err != nil {
		s.logger.Error("scheduler: load enabled monitors", "error", err)
		return
	}
	now := time.Now()
	for _, m := range monitors {
		due, seen := s.nextRun[m.ID]
		if seen && now.Before(due) {
			continue
		}
		interval := time.Duration(m.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		next := now.Add(interval + jitter(interval, 0.1))
		if !seen {
			next = now.Add(jitter(interval, 0.25))
		}
		s.nextRun[m.ID] = next

		regions := m.Regions
		if len(regions) == 0 {
			regions = []string{"default"}
		}
		// Encode the monitor once; cheap json round-trip but easier on
		// the worker than re-loading from Postgres.
		snapshot := monitorSnapshot(m)
		for _, region := range regions {
			job := queue.Job{
				JobID:        uuid.NewString(),
				MonitorID:    m.ID,
				Region:       region,
				DispatchedAt: now.UTC(),
				Monitor:      snapshot,
			}
			if err := s.queue.EnqueueCheck(ctx, region, job); err != nil {
				s.logger.Warn("scheduler: enqueue check failed",
					"monitor_id", m.ID, "region", region, "error", err)
			}
		}
	}
}

func monitorSnapshot(m models.Monitor) map[string]any {
	// Marshal-then-unmarshal so the worker sees the same JSON shape as
	// the public API; cheaper than reflecting field-by-field.
	b, _ := json.Marshal(m)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func jitter(d time.Duration, fraction float64) time.Duration {
	if d <= 0 || fraction <= 0 {
		return 0
	}
	if fraction > 1 {
		fraction = 1
	}
	span := float64(d) * fraction
	return time.Duration((mathrand.Float64() - 0.5) * 2 * span)
}
