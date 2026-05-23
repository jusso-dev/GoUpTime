// Package service contains business logic that sits between the HTTP/worker
// layers and the storage layer. MonitoringService is the orchestrator: it
// runs a check, persists the result, updates the monitor's status, and
// applies the incident state machine.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/notifications"
	"github.com/jusso-dev/uptime/internal/queue"
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
	// region tags every persisted check_result with the worker vantage
	// that produced it. Defaults to "default" when unset.
	region string
	// queueClient, when set, lets the worker publish per-region results
	// to queue:results so the cross-region aggregator can confirm
	// failures before opening incidents. nil disables publishing.
	queueClient *queue.Client
	// dispatcher, when set, enqueues incident events to the durable
	// outbox in addition to the legacy direct-webhook notify.Send path.
	// During the transition both paths run; remove notify once every
	// channel type is a Provider.
	dispatcher *notifications.Dispatcher
	// appBaseURL is included in dispatched events so Slack/email/push
	// recipients get a one-click link back to the incident page.
	appBaseURL string
}

func NewMonitoringService(store repository.Store, checkers checks.Registry, notifier *notifications.Service, m *metrics.Metrics, persist bool) *MonitoringService {
	return &MonitoringService{store: store, checkers: checkers, notify: notifier, metrics: m, persist: persist, region: "default"}
}

// WithQueue returns a service variant that publishes each completed
// check result to the supplied Redis queue. Used by the worker so the
// scheduler-side aggregator can fold per-region verdicts.
func (s *MonitoringService) WithQueue(q *queue.Client) *MonitoringService {
	clone := *s
	clone.queueClient = q
	return &clone
}

// WithDispatcher returns a service variant that also enqueues incident
// events to the notifications dispatcher (durable outbox) on opens and
// resolves. appBaseURL is included so the recipient sees a one-click
// deep link back to the incident.
func (s *MonitoringService) WithDispatcher(d *notifications.Dispatcher, appBaseURL string) *MonitoringService {
	clone := *s
	clone.dispatcher = d
	clone.appBaseURL = appBaseURL
	return &clone
}

// WithRegion returns the service tagging every result with the given
// region label. Used by the worker entry-point to stamp WORKER_REGION
// onto every persisted check_result for cross-region aggregation later.
func (s *MonitoringService) WithRegion(region string) *MonitoringService {
	if region == "" {
		region = "default"
	}
	clone := *s
	clone.region = region
	return &clone
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
	// Pin the store context to the monitor's organization so the repository
	// inserts the check result and any derived incident under the correct
	// tenant — regardless of who originated the surrounding request.
	storeCtx := ctx
	if monitor.OrganizationID != "" {
		storeCtx = auth.WithSystemOrg(ctx, monitor.OrganizationID)
	}
	result.OrganizationID = monitor.OrganizationID
	if result.Region == "" {
		result.Region = s.region
	}
	saved, err := s.store.CreateCheckResult(storeCtx, result)
	if err != nil {
		return result, fmt.Errorf("store check result: %w", err)
	}
	// Publish a lightweight verdict so the cross-region aggregator can
	// fold this result into its rolling window. Failure here is logged
	// but does not abort the check — Redis being down should never
	// hurt monitoring availability.
	if s.queueClient != nil && s.queueClient.Available() {
		_ = s.queueClient.PublishResult(storeCtx, queue.Result{
			MonitorID:      saved.MonitorID,
			OrganizationID: saved.OrganizationID,
			Region:         saved.Region,
			Success:        saved.Success,
			Status:         string(saved.Status),
			Error:          saved.Error,
			CheckedAt:      saved.CheckedAt,
		})
	}
	if err := s.applyIncidentRules(storeCtx, monitor, saved); err != nil {
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
		OrganizationID:      monitor.OrganizationID,
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
	if s.notify != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
			defer cancel()
			ctx = auth.WithSystemOrg(ctx, monitor.OrganizationID)
			s.notify.SendIncidentOpened(ctx, monitor, incident)
		}()
	}
	if s.dispatcher != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
			defer cancel()
			ctx = auth.WithSystemOrg(ctx, monitor.OrganizationID)
			if err := s.dispatcher.Enqueue(ctx, monitor.OrganizationID, incident.ID, notifications.Event{
				Type:        "incident.opened",
				IncidentID:  incident.ID,
				MonitorID:   monitor.ID,
				MonitorName: monitor.Name,
				Status:      string(models.StatusDown),
				Reason:      incident.Reason,
				StartedAt:   incident.StartedAt.UTC().Format(time.RFC3339),
				URL:         s.incidentURL(monitor.OrganizationID, incident.ID),
			}); err != nil {
				// Already logged inside Enqueue; the outbox still has the row.
				_ = err
			}
		}()
	}
}

func (s *MonitoringService) dispatchResolved(monitor models.Monitor, incident models.Incident) {
	if s.notify != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
			defer cancel()
			ctx = auth.WithSystemOrg(ctx, monitor.OrganizationID)
			s.notify.SendIncidentResolved(ctx, monitor, incident)
		}()
	}
	if s.dispatcher != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
			defer cancel()
			ctx = auth.WithSystemOrg(ctx, monitor.OrganizationID)
			resolved := ""
			if incident.ResolvedAt != nil {
				resolved = incident.ResolvedAt.UTC().Format(time.RFC3339)
			}
			_ = s.dispatcher.Enqueue(ctx, monitor.OrganizationID, incident.ID, notifications.Event{
				Type:        "incident.resolved",
				IncidentID:  incident.ID,
				MonitorID:   monitor.ID,
				MonitorName: monitor.Name,
				Status:      string(models.StatusUp),
				ResolvedAt:  resolved,
				URL:         s.incidentURL(monitor.OrganizationID, incident.ID),
			})
		}()
	}
}

func (s *MonitoringService) incidentURL(orgID, incidentID string) string {
	if s.appBaseURL == "" {
		return ""
	}
	return s.appBaseURL + "/orgs/" + orgID + "/incidents/" + incidentID
}

