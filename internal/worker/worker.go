package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
)

type Worker struct {
	cfg      config.Config
	store    repository.Store
	monitor  *service.MonitoringService
	metrics  *metrics.Metrics
	logger   *slog.Logger
	jobs     chan models.Monitor
	inFlight sync.Map
	nextRun  map[string]time.Time
}

func New(cfg config.Config, store repository.Store, monitor *service.MonitoringService, m *metrics.Metrics, logger *slog.Logger) *Worker {
	return &Worker{
		cfg:     cfg,
		store:   store,
		monitor: monitor,
		metrics: m,
		logger:  logger,
		jobs:    make(chan models.Monitor, cfg.CheckWorkerCount*2),
		nextRun: map[string]time.Time{},
	}
}

func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := 0; i < w.cfg.CheckWorkerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			w.consume(ctx, workerID)
		}(i + 1)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			close(w.jobs)
			wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			w.enqueueDue(ctx)
		}
	}
}

func (w *Worker) enqueueDue(ctx context.Context) {
	monitors, err := w.store.ListEnabledMonitors(ctx)
	if err != nil {
		w.logger.Error("load enabled monitors", "error", err)
		return
	}
	now := time.Now()
	for _, monitor := range monitors {
		if _, running := w.inFlight.Load(monitor.ID); running {
			continue
		}
		if due := w.nextRun[monitor.ID]; !due.IsZero() && now.Before(due) {
			continue
		}
		interval := time.Duration(monitor.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		w.nextRun[monitor.ID] = now.Add(interval)
		select {
		case w.jobs <- monitor:
		default:
			w.logger.Warn("worker queue full", "monitor_id", monitor.ID)
		}
	}
}

func (w *Worker) consume(ctx context.Context, workerID int) {
	for monitor := range w.jobs {
		w.inFlight.Store(monitor.ID, true)
		w.metrics.WorkerActive.Inc()
		result, err := w.monitor.RunCheck(ctx, monitor)
		w.metrics.WorkerActive.Dec()
		w.metrics.WorkerCompleted.Inc()
		if err != nil || !result.Success {
			w.metrics.WorkerFailed.Inc()
		}
		w.inFlight.Delete(monitor.ID)
		w.logger.Info("check completed", "worker_id", workerID, "monitor_id", monitor.ID, "status", result.Status, "success", result.Success, "duration_ms", result.TotalMS, "error", result.Error)
	}
}
