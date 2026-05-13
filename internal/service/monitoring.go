// Package service contains business logic that sits between the HTTP/worker
// layers and the storage layer. MonitoringService is the orchestrator: it
// runs a check, persists the result, updates the monitor's status, and
// applies the incident state machine.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/notifications"
	"github.com/jusso-dev/uptime/internal/repository"
)

// notificationTimeout caps the lifetime of a fire-and-forget notification
// dispatched after the originating check completes. Independent from the
// caller's context so a finishing HTTP request doesn't kill the webhook.
const notificationTimeout = 30 * time.Second

type MonitoringService struct {
	store    repository.Store
	checkers checks.Registry
	notify   *notifications.Service
	metrics  *metrics.Metrics
	persist  bool
}

func NewMonitoringService(store repository.Store, checkers checks.Registry, notifier *notifications.Service, m *metrics.Metrics, persist bool) *MonitoringService {
	return &MonitoringService{store: store, checkers: checkers, notify: notifier, metrics: m, persist: persist}
}

// RunCheck executes a single check, persists the result (if configured), and
// updates incident state. The returned error preserves the underlying check
// failure so callers can distinguish "check ran and target was down" (nil
// error, result.Success == false) from "we could not run the check" (non-nil
// error, result may be empty).
func (s *MonitoringService) RunCheck(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	checker, err := s.checkers.For(monitor.Type)
	if err != nil {
		return models.CheckResult{}, err
	}
	result, checkErr := checker.Check(ctx, monitor)
	if checkErr != nil && result.Error == "" {
		result.Error = checkErr.Error()
	}
	if result.Status == "" {
		if result.Success {
			result.Status = models.StatusUp
		} else {
			result.Status = models.StatusDown
		}
	}
	if s.metrics != nil {
		s.metrics.ObserveCheck(monitor, result)
	}
	if !s.persist || monitor.ID == "" || s.store == nil {
		return result, checkErr
	}
	saved, err := s.store.CreateCheckResult(ctx, result)
	if err != nil {
		return result, fmt.Errorf("store check result: %w", err)
	}
	if err := s.applyIncidentRules(ctx, monitor, saved); err != nil {
		return saved, err
	}
	return saved, checkErr
}

// applyIncidentRules implements the incident state machine:
//   - on success: clear any open incident and notify "resolved"
//   - on failure: bump consecutive failure count and open a new incident
//     once the configured threshold is reached
//
// Notifications are dispatched in a goroutine with a fresh context so they
// can outlive the originating request without exposing the caller's context
// to long network operations.
func (s *MonitoringService) applyIncidentRules(ctx context.Context, monitor models.Monitor, result models.CheckResult) error {
	status := result.Status
	if result.Success && status != models.StatusDegraded {
		status = models.StatusUp
	}
	if err := s.store.UpdateMonitorStatus(ctx, monitor.ID, status); err != nil {
		return fmt.Errorf("update monitor status: %w", err)
	}

	open, err := s.store.GetOpenIncident(ctx, monitor.ID)
	if err != nil {
		return fmt.Errorf("lookup open incident: %w", err)
	}
	if result.Success {
		if open != nil {
			resolved, err := s.store.ResolveIncident(ctx, open.ID)
			if err != nil {
				return fmt.Errorf("resolve incident: %w", err)
			}
			s.dispatchResolved(monitor, resolved)
		}
		return nil
	}

	failures, err := s.store.CountConsecutiveFailures(ctx, monitor.ID)
	if err != nil {
		return fmt.Errorf("count consecutive failures: %w", err)
	}
	threshold := monitor.FailureThreshold
	if threshold <= 0 {
		threshold = 3
	}
	if failures < threshold || open != nil {
		return nil
	}
	reason := result.Error
	if reason == "" {
		reason = "monitor check failed"
	}
	incident, err := s.store.OpenIncident(ctx, models.Incident{
		MonitorID:           monitor.ID,
		Status:              models.IncidentOpen,
		StartedAt:           time.Now().UTC(),
		Reason:              reason,
		LastError:           result.Error,
		ConsecutiveFailures: failures,
	})
	if err != nil {
		return fmt.Errorf("open incident: %w", err)
	}
	s.dispatchOpened(monitor, incident)
	return nil
}

func (s *MonitoringService) dispatchOpened(monitor models.Monitor, incident models.Incident) {
	if s.notify == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
		defer cancel()
		s.notify.SendIncidentOpened(ctx, monitor, incident)
	}()
}

func (s *MonitoringService) dispatchResolved(monitor models.Monitor, incident models.Incident) {
	if s.notify == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
		defer cancel()
		s.notify.SendIncidentResolved(ctx, monitor, incident)
	}()
}

