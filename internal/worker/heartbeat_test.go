package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
)

type fakeStore struct {
	mu    sync.Mutex
	beats []models.WorkerHeartbeat
	calls atomic.Int32
}

func (f *fakeStore) UpsertWorkerHeartbeat(_ context.Context, hb models.WorkerHeartbeat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls.Add(1)
	f.beats = append(f.beats, hb)
	return nil
}

func (f *fakeStore) latest() (models.WorkerHeartbeat, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.beats) == 0 {
		return models.WorkerHeartbeat{}, false
	}
	return f.beats[len(f.beats)-1], true
}

// Implement the rest of repository.Store as no-ops so we can satisfy the
// interface without dragging the postgres driver into worker tests.
func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) CreateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (f *fakeStore) ListMonitors(context.Context) ([]models.Monitor, error)        { return nil, nil }
func (f *fakeStore) ListEnabledMonitors(context.Context) ([]models.Monitor, error) { return nil, nil }
func (f *fakeStore) GetMonitor(context.Context, string) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (f *fakeStore) UpdateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (f *fakeStore) DeleteMonitor(context.Context, string) error                           { return nil }
func (f *fakeStore) UpdateMonitorStatus(context.Context, string, models.CheckStatus) error { return nil }
func (f *fakeStore) CreateCheckResult(context.Context, models.CheckResult) (models.CheckResult, error) {
	return models.CheckResult{}, nil
}
func (f *fakeStore) ListCheckResults(context.Context, models.ResultFilter) ([]models.CheckResult, error) {
	return nil, nil
}
func (f *fakeStore) CountConsecutiveFailures(context.Context, string) (int, error) { return 0, nil }
func (f *fakeStore) ListIncidents(context.Context) ([]models.Incident, error)      { return nil, nil }
func (f *fakeStore) GetIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (f *fakeStore) GetOpenIncident(context.Context, string) (*models.Incident, error) {
	return nil, nil
}
func (f *fakeStore) OpenIncident(context.Context, models.Incident) (models.Incident, error) {
	return models.Incident{}, nil
}
func (f *fakeStore) ResolveIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (f *fakeStore) ListNotificationChannels(context.Context) ([]models.NotificationChannel, error) {
	return nil, nil
}
func (f *fakeStore) GetNotificationChannel(context.Context, string) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (f *fakeStore) CreateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (f *fakeStore) UpdateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (f *fakeStore) DeleteNotificationChannel(context.Context, string) error { return nil }
func (f *fakeStore) LogNotificationEvent(context.Context, string, string, string, bool, int, string) error {
	return nil
}
func (f *fakeStore) CreateAPIKey(context.Context, models.APIKey) (models.APIKey, error) {
	return models.APIKey{}, nil
}
func (f *fakeStore) ListAPIKeys(context.Context) ([]models.APIKey, error) { return nil, nil }
func (f *fakeStore) FindAPIKeyByHash(context.Context, string) (*models.APIKey, error) {
	return nil, nil
}
func (f *fakeStore) TouchAPIKey(context.Context, string) error  { return nil }
func (f *fakeStore) RevokeAPIKey(context.Context, string) error { return nil }
func (f *fakeStore) OverviewStats(context.Context) (models.OverviewStats, error) {
	return models.OverviewStats{}, nil
}
func (f *fakeStore) ListWorkerHeartbeats(context.Context, time.Time) ([]models.WorkerHeartbeat, error) {
	return nil, nil
}
func (f *fakeStore) DeleteWorkerHeartbeat(context.Context, string) error { return nil }
func (f *fakeStore) AcknowledgeIncident(context.Context, string, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (f *fakeStore) GetOrganization(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (f *fakeStore) GetOrganizationByClerkID(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (f *fakeStore) UpsertOrganization(context.Context, models.Organization) (models.Organization, error) {
	return models.Organization{}, nil
}
func (f *fakeStore) DeleteOrganizationByClerkID(context.Context, string) error { return nil }
func (f *fakeStore) GetUserByID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (f *fakeStore) GetUserByClerkID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (f *fakeStore) UpsertUser(context.Context, models.User) (models.User, error) {
	return models.User{}, nil
}
func (f *fakeStore) DeleteUserByClerkID(context.Context, string) error { return nil }
func (f *fakeStore) ListMembershipsForUser(context.Context, string) ([]models.MembershipDetail, error) {
	return nil, nil
}
func (f *fakeStore) UpsertMembership(context.Context, models.Membership) error { return nil }
func (f *fakeStore) DeleteMembership(context.Context, string, string) error    { return nil }
func (f *fakeStore) RecordWebhookEvent(context.Context, string, string, []byte) (bool, error) {
	return true, nil
}
func (f *fakeStore) GetHeartbeat(context.Context, string) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (f *fakeStore) SetHeartbeat(context.Context, models.Heartbeat) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (f *fakeStore) DeleteHeartbeat(context.Context, string) error { return nil }
func (f *fakeStore) RecordHeartbeatPing(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (f *fakeStore) GetMultistepScript(context.Context, string) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (f *fakeStore) SetMultistepScript(context.Context, models.MultistepScript) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (f *fakeStore) GetBrowserScript(context.Context, string) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}
func (f *fakeStore) SetBrowserScript(context.Context, models.BrowserScript) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}

// sharedMetrics is built once because metrics.New uses promauto, which
// registers against the default registry — calling it twice would panic
// with "duplicate metrics collector registration attempted".
var sharedMetrics = metrics.New()

func newTestWorker(t *testing.T, store *fakeStore, workers int) *Worker {
	t.Helper()
	return New(
		config.Config{CheckWorkerCount: workers, Version: "test"},
		store,
		nil,
		sharedMetrics,
		slog.New(slog.NewTextHandler(testWriter{t: t}, nil)),
	)
}

func TestWriteHeartbeatPopulatesPoolState(t *testing.T) {
	store := &fakeStore{}
	w := newTestWorker(t, store, 4)
	w.activeJobs.Store(2)
	w.jobsCompleted.Store(10)
	w.jobsFailed.Store(1)
	w.inFlight.Store("mon-1", struct{}{})
	w.inFlight.Store("mon-2", struct{}{})

	if err := w.writeHeartbeat(context.Background()); err != nil {
		t.Fatalf("writeHeartbeat: %v", err)
	}
	hb, ok := store.latest()
	if !ok {
		t.Fatal("no heartbeat recorded")
	}
	if hb.InstanceID != w.instanceID {
		t.Fatalf("instance id mismatch: %q vs %q", hb.InstanceID, w.instanceID)
	}
	if hb.WorkerCount != 4 || hb.ActiveJobs != 2 || hb.JobsCompleted != 10 || hb.JobsFailed != 1 {
		t.Fatalf("unexpected counters: %+v", hb)
	}
	if len(hb.InFlight) != 2 {
		t.Fatalf("expected 2 in-flight, got %v", hb.InFlight)
	}
	if hb.QueueCapacity == 0 {
		t.Fatalf("expected non-zero queue capacity")
	}
}

func TestSnapshotInFlightIsConsistent(t *testing.T) {
	w := newTestWorker(t, &fakeStore{}, 1)
	for _, id := range []string{"a", "b", "c"} {
		w.inFlight.Store(id, struct{}{})
	}
	got := w.snapshotInFlight()
	if len(got) != 3 {
		t.Fatalf("expected 3 ids, got %v", got)
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !seen[want] {
			t.Fatalf("missing id %q in snapshot %v", want, got)
		}
	}
}

func TestHeartbeatLoopWritesAtLeastOnce(t *testing.T) {
	store := &fakeStore{}
	w := newTestWorker(t, store, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w.runHeartbeats(ctx)
	if store.calls.Load() < 1 {
		t.Fatalf("expected at least one heartbeat write")
	}
}

// testWriter routes log output to t.Log so the harness preserves it on
// failure but stays quiet on success.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
