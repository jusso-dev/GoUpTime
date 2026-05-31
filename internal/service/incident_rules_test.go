package service

import (
	"context"
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
)

func TestIncidentThresholdLogic(t *testing.T) {
	store := &fakeIncidentStore{failures: 2}
	svc := &MonitoringService{store: store}
	monitor := models.Monitor{ID: "m1", FailureThreshold: 3}
	result := models.CheckResult{MonitorID: "m1", Status: models.StatusDown, Success: false, Error: "boom"}
	if err := svc.applyIncidentRules(context.Background(), monitor, result); err != nil {
		t.Fatal(err)
	}
	if store.opened {
		t.Fatal("incident opened before threshold")
	}
	store.failures = 3
	if err := svc.applyIncidentRules(context.Background(), monitor, result); err != nil {
		t.Fatal(err)
	}
	if !store.opened {
		t.Fatal("incident should open at threshold")
	}
}

func TestIncidentResolutionAfterRecovery(t *testing.T) {
	store := &fakeIncidentStore{open: &models.Incident{ID: "i1", MonitorID: "m1", Status: models.IncidentOpen}}
	svc := &MonitoringService{store: store}
	result := models.CheckResult{MonitorID: "m1", Status: models.StatusUp, Success: true}
	if err := svc.applyIncidentRules(context.Background(), models.Monitor{ID: "m1"}, result); err != nil {
		t.Fatal(err)
	}
	if !store.resolved {
		t.Fatal("expected open incident to resolve")
	}
}

func TestRunCheckSuppressesIncidentDuringMaintenance(t *testing.T) {
	store := &fakeIncidentStore{
		failures: 3,
		maintenance: &models.MaintenanceWindow{
			ID:     "mw1",
			Name:   "deploy",
			Active: true,
		},
	}
	svc := NewMonitoringService(store, checks.Registry{HTTP: fakeChecker{}}, nil, nil, true)
	result, err := svc.RunCheck(context.Background(), models.Monitor{
		ID:               "m1",
		Type:             models.MonitorHTTP,
		Target:           "https://example.com",
		FailureThreshold: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.MaintenanceSuppressed {
		t.Fatalf("expected maintenance suppression: %+v", result)
	}
	if store.opened {
		t.Fatal("incident should not open during maintenance")
	}
}

func TestRegionalQuorumSuppressesUntilEnoughRegionsFail(t *testing.T) {
	store := &fakeIncidentStore{
		failures: 1,
		results: []models.CheckResult{
			{MonitorID: "m1", Region: "syd", Success: false, Status: models.StatusDown, CheckedAt: time.Now().UTC()},
		},
	}
	svc := &MonitoringService{store: store}
	err := svc.applyIncidentRules(context.Background(),
		models.Monitor{ID: "m1", FailureThreshold: 1, RegionConfirmationThreshold: 2, IntervalSeconds: 60},
		models.CheckResult{MonitorID: "m1", Region: "syd", Success: false, Status: models.StatusDown, Error: "timeout"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.opened {
		t.Fatal("incident should not open before regional quorum")
	}
	if store.lastStatus != models.StatusDegraded {
		t.Fatalf("expected degraded regional status, got %q", store.lastStatus)
	}
	if len(store.suppressions) == 0 || store.suppressions[0].Reason != "regional_quorum" {
		t.Fatalf("expected regional quorum suppression, got %+v", store.suppressions)
	}
}

func TestRegionalQuorumOpensWhenThresholdMet(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeIncidentStore{
		failures: 1,
		results: []models.CheckResult{
			{MonitorID: "m1", Region: "syd", Success: false, Status: models.StatusDown, CheckedAt: now},
			{MonitorID: "m1", Region: "iad", Success: false, Status: models.StatusDown, CheckedAt: now},
		},
	}
	svc := &MonitoringService{store: store}
	err := svc.applyIncidentRules(context.Background(),
		models.Monitor{ID: "m1", FailureThreshold: 1, RegionConfirmationThreshold: 2, IntervalSeconds: 60},
		models.CheckResult{MonitorID: "m1", Region: "syd", Success: false, Status: models.StatusDown, Error: "timeout"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !store.opened {
		t.Fatal("incident should open when regional quorum is met")
	}
}

func TestDependencySuppressionPreventsChildIncident(t *testing.T) {
	store := &fakeIncidentStore{
		failures: 1,
		deps:     []models.MonitorDependency{{MonitorID: "child", DependsOnMonitorID: "parent"}},
		openByMonitor: map[string]*models.Incident{
			"parent": {ID: "i-parent", MonitorID: "parent", Status: models.IncidentOpen},
		},
	}
	svc := &MonitoringService{store: store}
	err := svc.applyIncidentRules(context.Background(),
		models.Monitor{ID: "child", FailureThreshold: 1},
		models.CheckResult{MonitorID: "child", Success: false, Status: models.StatusDown, Error: "connect failed"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.opened {
		t.Fatal("child incident should be suppressed while parent dependency is down")
	}
	if len(store.suppressions) == 0 || store.suppressions[0].Reason != "dependency" {
		t.Fatalf("expected dependency suppression, got %+v", store.suppressions)
	}
}

func TestFlappingSuppressionPreventsAlertStorm(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeIncidentStore{
		failures: 1,
		results: []models.CheckResult{
			{MonitorID: "m1", Success: false, CheckedAt: now},
			{MonitorID: "m1", Success: true, CheckedAt: now.Add(-1 * time.Minute)},
			{MonitorID: "m1", Success: false, CheckedAt: now.Add(-2 * time.Minute)},
			{MonitorID: "m1", Success: true, CheckedAt: now.Add(-3 * time.Minute)},
			{MonitorID: "m1", Success: false, CheckedAt: now.Add(-4 * time.Minute)},
			{MonitorID: "m1", Success: true, CheckedAt: now.Add(-5 * time.Minute)},
		},
	}
	svc := &MonitoringService{store: store}
	err := svc.applyIncidentRules(context.Background(),
		models.Monitor{ID: "m1", FailureThreshold: 1},
		models.CheckResult{MonitorID: "m1", Success: false, Status: models.StatusDown, Error: "alternating"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.opened {
		t.Fatal("flapping monitor should be cooled down instead of opening a new incident")
	}
	if len(store.suppressions) == 0 || store.suppressions[0].Reason != "flapping" {
		t.Fatalf("expected flapping suppression, got %+v", store.suppressions)
	}
}

type fakeIncidentStore struct {
	repository.NoopStore
	failures      int
	open          *models.Incident
	openByMonitor map[string]*models.Incident
	opened        bool
	resolved      bool
	maintenance   *models.MaintenanceWindow
	results       []models.CheckResult
	deps          []models.MonitorDependency
	suppressions  []models.IncidentSuppression
	lastStatus    models.CheckStatus
}

func (f *fakeIncidentStore) UpdateMonitorStatus(_ context.Context, _ string, status models.CheckStatus) error {
	f.lastStatus = status
	return nil
}

func (f *fakeIncidentStore) GetOpenIncident(_ context.Context, monitorID string) (*models.Incident, error) {
	if f.openByMonitor != nil {
		return f.openByMonitor[monitorID], nil
	}
	return f.open, nil
}

func (f *fakeIncidentStore) CountConsecutiveFailures(context.Context, string) (int, error) {
	return f.failures, nil
}

func (f *fakeIncidentStore) OpenIncident(_ context.Context, i models.Incident) (models.Incident, error) {
	f.opened = true
	f.open = &i
	return i, nil
}

func (f *fakeIncidentStore) ResolveIncident(_ context.Context, id string) (models.Incident, error) {
	f.resolved = true
	return models.Incident{ID: id, Status: models.IncidentResolved}, nil
}

func (f *fakeIncidentStore) ActiveMaintenanceForMonitor(context.Context, string, time.Time) (*models.MaintenanceWindow, error) {
	return f.maintenance, nil
}

func (f *fakeIncidentStore) CreateCheckResult(_ context.Context, result models.CheckResult) (models.CheckResult, error) {
	return result, nil
}

func (f *fakeIncidentStore) ListCheckResults(context.Context, models.ResultFilter) ([]models.CheckResult, error) {
	return f.results, nil
}

func (f *fakeIncidentStore) ListMonitorDependencies(context.Context, string) ([]models.MonitorDependency, error) {
	return f.deps, nil
}

func (f *fakeIncidentStore) RecordIncidentSuppression(_ context.Context, suppression models.IncidentSuppression) (models.IncidentSuppression, error) {
	f.suppressions = append(f.suppressions, suppression)
	return suppression, nil
}

type fakeChecker struct{}

func (fakeChecker) Check(context.Context, models.Monitor) (models.CheckResult, error) {
	return models.CheckResult{
		MonitorID: "m1",
		Status:    models.StatusDown,
		Success:   false,
		Error:     "planned outage",
		CheckedAt: time.Now().UTC(),
	}, nil
}
