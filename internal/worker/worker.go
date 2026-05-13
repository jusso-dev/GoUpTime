// Package worker drives the periodic execution of monitor checks. The
// scheduler reads enabled monitors from the store, decides which are due,
// and fans them out to a fixed pool of goroutines that perform the checks
// and persist results. Designed for a single-process model; distributed
// scheduling would replace enqueueDue with a Redis-backed lock + queue.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
)

type Worker struct {
	cfg     config.Config
	store   repository.Store
	monitor *service.MonitoringService
	metrics *metrics.Metrics
	logger  *slog.Logger
	jobs    chan models.Monitor
	// inFlight tracks monitor IDs currently being processed. sync.Map fits
	// the read-mostly access pattern (one writer per monitor, many readers
	// in enqueueDue).
	inFlight sync.Map
	// nextRun is only mutated from the single Run goroutine, so no
	// synchronization is needed.
	nextRun map[string]time.Time
}

func New(cfg config.Config, store repository.Store, monitor *service.MonitoringService, m *metrics.Metrics, logger *slog.Logger) *Worker {
	bufferSize := cfg.CheckWorkerCount * 2
	if bufferSize < 4 {
		bufferSize = 4
	}
	return &Worker{
		cfg:     cfg,
		store:   store,
		monitor: monitor,
		metrics: m,
		logger:  logger,
		jobs:    make(chan models.Monitor, bufferSize),
		nextRun: map[string]time.Time{},
	}
}

// Run starts the worker pool and blocks until ctx is cancelled. It returns
// ctx.Err() on shutdown so callers can distinguish a clean shutdown from a
// programmer error. The function is not safe to call more than once.
func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := 0; i < w.cfg.CheckWorkerCount; i++ {
		wg.Add(1)
		workerID := i + 1
		go func() {
			defer wg.Done()
			w.consume(ctx, workerID)
		}()
	}

	tick := w.cfg.SchedulerTick()
	if tick <= 0 {
		tick = 5 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	// Initial enqueue without waiting a full tick — better cold start
	// behaviour, especially for low-frequency checks.
	w.enqueueDue(ctx)
	w.logger.Info("worker started", "workers", w.cfg.CheckWorkerCount, "tick", tick.String())

	for {
		select {
		case <-ctx.Done():
			close(w.jobs)
			wg.Wait()
			w.logger.Info("worker stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			w.enqueueDue(ctx)
		}
	}
}

// enqueueDue scans enabled monitors and submits any whose next-run time has
// passed and that are not currently in flight. New monitors get a small
// random startup delay so a hundred reboots don't all hammer their targets
// at the same wall-clock moment.
func (w *Worker) enqueueDue(ctx context.Context) {
	monitors, err := w.store.ListEnabledMonitors(ctx)
	if err != nil {
		w.logger.Error("scheduler: load enabled monitors", "error", err)
		return
	}
	now := time.Now()
	for _, monitor := range monitors {
		if _, running := w.inFlight.Load(monitor.ID); running {
			continue
		}
		due, seen := w.nextRun[monitor.ID]
		if seen && now.Before(due) {
			continue
		}
		interval := time.Duration(monitor.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		// Jitter ±10% to smooth out herd behaviour. For first-seen
		// monitors we add a one-shot startup jitter up to 1/4 of the
		// interval so a fresh worker doesn't immediately enqueue every
		// monitor at once.
		next := now.Add(interval + jitter(interval, 0.1))
		if !seen {
			next = now.Add(jitter(interval, 0.25))
		}
		w.nextRun[monitor.ID] = next

		select {
		case w.jobs <- monitor:
		case <-ctx.Done():
			return
		default:
			// Queue full means workers can't keep up; reset the next
			// run so we re-attempt soon rather than waiting a full
			// interval, and emit a warn so operators can spot it.
			w.nextRun[monitor.ID] = now.Add(time.Second)
			w.logger.Warn("scheduler: jobs queue full, will retry shortly",
				"monitor_id", monitor.ID, "queue_capacity", cap(w.jobs))
		}
	}
}

// consume processes jobs until the channel is closed. Each job is run with
// panic recovery so a single misbehaving check cannot bring down the worker.
func (w *Worker) consume(ctx context.Context, workerID int) {
	for monitor := range w.jobs {
		w.runOne(ctx, workerID, monitor)
	}
}

func (w *Worker) runOne(ctx context.Context, workerID int, monitor models.Monitor) {
	w.inFlight.Store(monitor.ID, struct{}{})
	defer w.inFlight.Delete(monitor.ID)
	w.metrics.WorkerActive.Inc()
	defer w.metrics.WorkerActive.Dec()

	defer func() {
		if r := recover(); r != nil {
			w.metrics.WorkerFailed.Inc()
			w.logger.Error("check panic recovered",
				"worker_id", workerID,
				"monitor_id", monitor.ID,
				"panic", fmt.Sprint(r),
				"stack", string(debug.Stack()),
			)
		}
	}()

	start := time.Now()
	result, err := w.monitor.RunCheck(ctx, monitor)
	w.metrics.WorkerCompleted.Inc()
	if err != nil || !result.Success {
		w.metrics.WorkerFailed.Inc()
	}

	level := slog.LevelInfo
	if !result.Success {
		level = slog.LevelWarn
	}
	w.logger.Log(ctx, level, "check completed",
		"worker_id", workerID,
		"monitor_id", monitor.ID,
		"monitor_type", monitor.Type,
		"status", result.Status,
		"success", result.Success,
		"status_code", result.StatusCode,
		"duration_ms", result.TotalMS,
		"queued_ms", time.Since(start).Milliseconds()-result.TotalMS,
		"error", result.Error,
	)
}

// jitter returns a uniformly-distributed offset in the range
// [-fraction*d, +fraction*d]. fraction is clamped to [0, 1].
func jitter(d time.Duration, fraction float64) time.Duration {
	if d <= 0 || fraction <= 0 {
		return 0
	}
	if fraction > 1 {
		fraction = 1
	}
	span := float64(d) * fraction
	// mathrand.Float64() in [0, 1); shift to [-0.5, 0.5) then scale.
	return time.Duration((mathrand.Float64() - 0.5) * 2 * span)
}
