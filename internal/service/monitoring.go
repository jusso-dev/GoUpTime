// Package service contains business logic that sits between the HTTP/worker
// layers and the storage layer. MonitoringService is the orchestrator: it
// runs a check, persists the result, updates the monitor's status, and
// applies the incident state machine.
package service

import (
	"context"
	"fmt"
	"strings"
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

type incidentTimelineStore interface {
	RecordIncidentTimeline(context.Context, models.IncidentTimelineEvent) (models.IncidentTimelineEvent, error)
}

type incidentSuppressionStore interface {
	RecordIncidentSuppression(context.Context, models.IncidentSuppression) (models.IncidentSuppression, error)
}

type incidentFailureUpdater interface {
	UpdateIncidentFailure(context.Context, string, string, int) (models.Incident, error)
}

type monitorDependencyStore interface {
	ListMonitorDependencies(context.Context, string) ([]models.MonitorDependency, error)
}

type statusIncidentUpdateStore interface {
	AutoCreateStatusPageIncidentUpdate(context.Context, models.Incident, string, string) error
}

type escalationResolver interface {
	ResolveEscalationPolicy(context.Context, models.Monitor, models.Incident) (*models.EscalationPolicy, error)
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
	if active, err := s.store.ActiveMaintenanceForMonitor(storeCtx, monitor.ID, time.Now().UTC()); err != nil {
		return result, fmt.Errorf("lookup maintenance windows: %w", err)
	} else if active != nil && !result.Success {
		result.MaintenanceSuppressed = true
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["maintenanceWindowId"] = active.ID
		result.Metadata["maintenanceWindowName"] = active.Name
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

// RecordResult persists a result produced outside the local checker process
// (for example by a private agent) and runs the same incident rules as
// RunCheck.
func (s *MonitoringService) RecordResult(ctx context.Context, monitor models.Monitor, result models.CheckResult) (models.CheckResult, error) {
	if !s.persist || monitor.ID == "" || s.store == nil {
		return result, nil
	}
	storeCtx := ctx
	if monitor.OrganizationID != "" {
		storeCtx = auth.WithSystemOrg(ctx, monitor.OrganizationID)
	}
	result.MonitorID = monitor.ID
	result.OrganizationID = monitor.OrganizationID
	if result.CheckedAt.IsZero() {
		result.CheckedAt = time.Now().UTC()
	}
	if result.Status == "" {
		if result.Success {
			result.Status = models.StatusUp
		} else {
			result.Status = models.StatusDown
		}
	}
	if result.Region == "" {
		result.Region = s.region
	}
	if active, err := s.store.ActiveMaintenanceForMonitor(storeCtx, monitor.ID, time.Now().UTC()); err != nil {
		return result, fmt.Errorf("lookup maintenance windows: %w", err)
	} else if active != nil && !result.Success {
		result.MaintenanceSuppressed = true
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["maintenanceWindowId"] = active.ID
		result.Metadata["maintenanceWindowName"] = active.Name
	}
	saved, err := s.store.CreateCheckResult(storeCtx, result)
	if err != nil {
		return result, fmt.Errorf("store check result: %w", err)
	}
	if err := s.applyIncidentRules(storeCtx, monitor, saved); err != nil {
		return saved, err
	}
	return saved, nil
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
			s.recordTimeline(ctx, models.IncidentTimelineEvent{
				IncidentID: resolved.ID,
				EventType:  "check.recovered",
				Evidence:   evidenceFromResult(result),
			})
			s.publishStatusUpdate(ctx, resolved, monitor, "Incident resolved", "The affected monitor has recovered.")
			s.dispatchResolved(monitor, resolved)
		}
		return nil
	}
	if result.MaintenanceSuppressed {
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
		if open != nil {
			s.updateExistingIncident(ctx, open.ID, result.Error, failures, result)
		}
		return nil
	}
	if suppressed, reason, err := s.suppressedByDependency(ctx, monitor); err != nil {
		return err
	} else if suppressed {
		s.recordSuppression(ctx, monitor.ID, "dependency", reason)
		_ = s.store.UpdateMonitorStatus(ctx, monitor.ID, models.StatusDegraded)
		return nil
	}
	if suppressed, reason, err := s.suppressedByRegionalQuorum(ctx, monitor); err != nil {
		return err
	} else if suppressed {
		s.recordSuppression(ctx, monitor.ID, "regional_quorum", reason)
		_ = s.store.UpdateMonitorStatus(ctx, monitor.ID, models.StatusDegraded)
		return nil
	}
	if s.isFlapping(ctx, monitor.ID) {
		s.recordSuppression(ctx, monitor.ID, "flapping", "monitor changed state repeatedly during the cooldown window")
		_ = s.store.UpdateMonitorStatus(ctx, monitor.ID, models.StatusDegraded)
		return nil
	}
	reason := result.Error
	if reason == "" {
		reason = "monitor check failed"
	}
	severity, impact := incidentSeverityImpact(monitor, result)
	incident, err := s.store.OpenIncident(ctx, models.Incident{
		OrganizationID:      monitor.OrganizationID,
		MonitorID:           monitor.ID,
		Status:              models.IncidentOpen,
		Severity:            severity,
		Impact:              impact,
		StartedAt:           time.Now().UTC(),
		Reason:              reason,
		LastError:           result.Error,
		GroupKey:            incidentGroupKey(monitor, result),
		ErrorClass:          errorClass(result.Error),
		ConsecutiveFailures: failures,
	})
	if err != nil {
		return fmt.Errorf("open incident: %w", err)
	}
	s.recordTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID: incident.ID,
		EventType:  "check.failed",
		Evidence:   evidenceFromResult(result),
	})
	s.publishStatusUpdate(ctx, incident, monitor, "Incident detected", reason)
	s.recordEscalationPlan(ctx, monitor, incident)
	s.dispatchOpened(monitor, incident)
	return nil
}

func (s *MonitoringService) updateExistingIncident(ctx context.Context, incidentID, lastError string, failures int, result models.CheckResult) {
	if updater, ok := s.store.(incidentFailureUpdater); ok {
		_, _ = updater.UpdateIncidentFailure(ctx, incidentID, lastError, failures)
	}
	s.recordTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID: incidentID,
		EventType:  "check.failed",
		Evidence:   evidenceFromResult(result),
		Metadata:   map[string]any{"consecutiveFailures": failures},
	})
}

func (s *MonitoringService) suppressedByDependency(ctx context.Context, monitor models.Monitor) (bool, string, error) {
	depsStore, ok := s.store.(monitorDependencyStore)
	if !ok {
		return false, "", nil
	}
	deps, err := depsStore.ListMonitorDependencies(ctx, monitor.ID)
	if err != nil {
		return false, "", fmt.Errorf("list monitor dependencies: %w", err)
	}
	for _, dep := range deps {
		parent, err := s.store.GetOpenIncident(ctx, dep.DependsOnMonitorID)
		if err != nil {
			return false, "", fmt.Errorf("lookup dependency incident: %w", err)
		}
		if parent != nil {
			return true, "parent dependency " + dep.DependsOnMonitorID + " has active incident " + parent.ID, nil
		}
	}
	return false, "", nil
}

func (s *MonitoringService) suppressedByRegionalQuorum(ctx context.Context, monitor models.Monitor) (bool, string, error) {
	threshold := monitor.RegionConfirmationThreshold
	if threshold <= 1 {
		return false, "", nil
	}
	results, err := s.store.ListCheckResults(ctx, models.ResultFilter{MonitorID: monitor.ID, Limit: 100})
	if err != nil {
		return false, "", fmt.Errorf("load regional check results: %w", err)
	}
	latest := map[string]models.CheckResult{}
	cutoff := time.Now().UTC().Add(-regionalWindow(monitor))
	for _, result := range results {
		region := result.Region
		if region == "" {
			region = "default"
		}
		if result.CheckedAt.Before(cutoff) {
			continue
		}
		if _, ok := latest[region]; !ok {
			latest[region] = result
		}
	}
	failures := 0
	for _, result := range latest {
		if !result.Success {
			failures++
		}
	}
	if failures < threshold {
		return true, fmt.Sprintf("%d regional failures below quorum %d", failures, threshold), nil
	}
	return false, "", nil
}

func regionalWindow(monitor models.Monitor) time.Duration {
	interval := monitor.IntervalSeconds
	if interval <= 0 {
		interval = 60
	}
	return time.Duration(interval*3) * time.Second
}

func (s *MonitoringService) isFlapping(ctx context.Context, monitorID string) bool {
	results, err := s.store.ListCheckResults(ctx, models.ResultFilter{MonitorID: monitorID, Limit: 10})
	if err != nil || len(results) < 6 {
		return false
	}
	transitions := 0
	last := results[0].Success
	for _, result := range results[1:] {
		if result.Success != last {
			transitions++
		}
		last = result.Success
	}
	return transitions >= 4
}

func (s *MonitoringService) recordSuppression(ctx context.Context, monitorID, reason, details string) {
	if store, ok := s.store.(incidentSuppressionStore); ok {
		_, _ = store.RecordIncidentSuppression(ctx, models.IncidentSuppression{
			MonitorID: monitorID,
			Reason:    reason,
			Details:   details,
		})
	}
}

func (s *MonitoringService) recordTimeline(ctx context.Context, event models.IncidentTimelineEvent) {
	if store, ok := s.store.(incidentTimelineStore); ok {
		_, _ = store.RecordIncidentTimeline(ctx, event)
	}
}

func (s *MonitoringService) publishStatusUpdate(ctx context.Context, incident models.Incident, monitor models.Monitor, title, body string) {
	if store, ok := s.store.(statusIncidentUpdateStore); ok {
		_ = store.AutoCreateStatusPageIncidentUpdate(ctx, incident, title, body)
	}
}

func (s *MonitoringService) recordEscalationPlan(ctx context.Context, monitor models.Monitor, incident models.Incident) {
	resolver, ok := s.store.(escalationResolver)
	if !ok {
		return
	}
	policy, err := resolver.ResolveEscalationPolicy(ctx, monitor, incident)
	if err != nil || policy == nil {
		return
	}
	s.recordTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID: incident.ID,
		EventType:  "incident.escalation_scheduled",
		Metadata: map[string]any{
			"policyId": policy.ID,
			"steps":    len(policy.Steps),
		},
	})
}

func incidentSeverityImpact(monitor models.Monitor, result models.CheckResult) (models.IncidentSeverity, models.IncidentImpact) {
	severity := models.SeverityMajor
	impact := models.ImpactDegraded
	if value, ok := monitor.Config["severity"].(string); ok {
		severity = models.IncidentSeverity(value)
	}
	if value, ok := monitor.Config["impact"].(string); ok {
		impact = models.IncidentImpact(value)
	}
	if result.Status == models.StatusDown && severity == models.SeverityInfo {
		severity = models.SeverityWarning
	}
	return severity, impact
}

func incidentGroupKey(monitor models.Monitor, result models.CheckResult) string {
	if value, ok := monitor.Config["groupKey"].(string); ok && value != "" {
		return value
	}
	parts := []string{"monitor", monitor.ID}
	if monitor.ServiceID != "" {
		parts = append(parts, "service", monitor.ServiceID)
	}
	if cls := errorClass(result.Error); cls != "" {
		parts = append(parts, "error", cls)
	}
	return strings.Join(parts, ":")
}

func errorClass(message string) string {
	message = strings.ToLower(message)
	switch {
	case message == "":
		return ""
	case strings.Contains(message, "timeout") || strings.Contains(message, "deadline"):
		return "timeout"
	case strings.Contains(message, "tls") || strings.Contains(message, "certificate"):
		return "tls"
	case strings.Contains(message, "dns") || strings.Contains(message, "no such host"):
		return "dns"
	case strings.Contains(message, "connection refused") || strings.Contains(message, "connect"):
		return "connectivity"
	default:
		return "check_failure"
	}
}

func evidenceFromResult(result models.CheckResult) map[string]any {
	evidence := map[string]any{
		"checkResultId":         result.ID,
		"monitorId":             result.MonitorID,
		"status":                result.Status,
		"success":               result.Success,
		"responseTimeMs":        result.ResponseTimeMS,
		"statusCode":            result.StatusCode,
		"error":                 result.Error,
		"checkedAt":             result.CheckedAt,
		"region":                result.Region,
		"dnsMs":                 result.DNSMS,
		"tcpConnectMs":          result.TCPConnectMS,
		"tlsHandshakeMs":        result.TLSHandshakeMS,
		"timeToFirstByteMs":     result.TimeToFirstByteMS,
		"totalMs":               result.TotalMS,
		"rootCauseLabel":        errorClass(result.Error),
		"maintenanceSuppressed": result.MaintenanceSuppressed,
	}
	if result.ResponseSnippet != "" {
		evidence["bodySnippet"] = result.ResponseSnippet
	}
	for key, value := range result.Metadata {
		evidence[key] = value
	}
	return evidence
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
				Severity:    string(incident.Severity),
				Impact:      string(incident.Impact),
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
				Severity:    string(incident.Severity),
				Impact:      string(incident.Impact),
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
