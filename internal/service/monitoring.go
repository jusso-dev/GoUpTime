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

func (s *MonitoringService) applyIncidentRules(ctx context.Context, monitor models.Monitor, result models.CheckResult) error {
	status := result.Status
	if result.Success && status != models.StatusDegraded {
		status = models.StatusUp
	}
	if err := s.store.UpdateMonitorStatus(ctx, monitor.ID, status); err != nil {
		return err
	}

	open, err := s.store.GetOpenIncident(ctx, monitor.ID)
	if err != nil {
		return err
	}
	if result.Success {
		if open != nil {
			resolved, err := s.store.ResolveIncident(ctx, open.ID)
			if err != nil {
				return err
			}
			if s.notify != nil {
				go s.notify.SendIncidentResolved(context.Background(), monitor, resolved)
			}
		}
		return nil
	}

	failures, err := s.store.CountConsecutiveFailures(ctx, monitor.ID)
	if err != nil {
		return err
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
		return err
	}
	if s.notify != nil {
		go s.notify.SendIncidentOpened(context.Background(), monitor, incident)
	}
	return nil
}
