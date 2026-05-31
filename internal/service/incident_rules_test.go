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

type fakeIncidentStore struct {
	repository.NoopStore
	failures    int
	open        *models.Incident
	opened      bool
	resolved    bool
	maintenance *models.MaintenanceWindow
}

func (f *fakeIncidentStore) UpdateMonitorStatus(context.Context, string, models.CheckStatus) error {
	return nil
}

func (f *fakeIncidentStore) GetOpenIncident(context.Context, string) (*models.Incident, error) {
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
