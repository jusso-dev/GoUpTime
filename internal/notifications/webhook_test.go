package notifications

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/models"
)

type recordingStore struct {
	mu       sync.Mutex
	channels []models.NotificationChannel
	events   []logEvent
}

type logEvent struct {
	channelID  string
	success    bool
	statusCode int
	errText    string
}

func (r *recordingStore) ListNotificationChannels(context.Context) ([]models.NotificationChannel, error) {
	return r.channels, nil
}

func (r *recordingStore) LogNotificationEvent(_ context.Context, channelID, _, _ string, success bool, statusCode int, errText string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, logEvent{channelID, success, statusCode, errText})
	return nil
}

// Implement the rest of the Store interface as no-ops so we can pass it where
// repository.Store is required.
func (r *recordingStore) Ping(context.Context) error { return nil }
func (r *recordingStore) CreateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (r *recordingStore) ListMonitors(context.Context) ([]models.Monitor, error)        { return nil, nil }
func (r *recordingStore) ListEnabledMonitors(context.Context) ([]models.Monitor, error) { return nil, nil }
func (r *recordingStore) GetMonitor(context.Context, string) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (r *recordingStore) UpdateMonitor(context.Context, models.Monitor) (models.Monitor, error) {
	return models.Monitor{}, nil
}
func (r *recordingStore) DeleteMonitor(context.Context, string) error { return nil }
func (r *recordingStore) UpdateMonitorStatus(context.Context, string, models.CheckStatus) error {
	return nil
}
func (r *recordingStore) CreateCheckResult(context.Context, models.CheckResult) (models.CheckResult, error) {
	return models.CheckResult{}, nil
}
func (r *recordingStore) ListCheckResults(context.Context, models.ResultFilter) ([]models.CheckResult, error) {
	return nil, nil
}
func (r *recordingStore) CountConsecutiveFailures(context.Context, string) (int, error) {
	return 0, nil
}
func (r *recordingStore) ListIncidents(context.Context) ([]models.Incident, error) { return nil, nil }
func (r *recordingStore) GetIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (r *recordingStore) GetOpenIncident(context.Context, string) (*models.Incident, error) {
	return nil, nil
}
func (r *recordingStore) OpenIncident(context.Context, models.Incident) (models.Incident, error) {
	return models.Incident{}, nil
}
func (r *recordingStore) ResolveIncident(context.Context, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (r *recordingStore) GetNotificationChannel(context.Context, string) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (r *recordingStore) CreateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (r *recordingStore) UpdateNotificationChannel(context.Context, models.NotificationChannel) (models.NotificationChannel, error) {
	return models.NotificationChannel{}, nil
}
func (r *recordingStore) DeleteNotificationChannel(context.Context, string) error { return nil }
func (r *recordingStore) CreateAPIKey(context.Context, models.APIKey) (models.APIKey, error) {
	return models.APIKey{}, nil
}
func (r *recordingStore) ListAPIKeys(context.Context) ([]models.APIKey, error) { return nil, nil }
func (r *recordingStore) FindAPIKeyByHash(context.Context, string) (*models.APIKey, error) {
	return nil, nil
}
func (r *recordingStore) TouchAPIKey(context.Context, string) error  { return nil }
func (r *recordingStore) RevokeAPIKey(context.Context, string) error { return nil }
func (r *recordingStore) OverviewStats(context.Context) (models.OverviewStats, error) {
	return models.OverviewStats{}, nil
}
func (r *recordingStore) UpsertWorkerHeartbeat(context.Context, models.WorkerHeartbeat) error {
	return nil
}
func (r *recordingStore) ListWorkerHeartbeats(context.Context, time.Time) ([]models.WorkerHeartbeat, error) {
	return nil, nil
}
func (r *recordingStore) DeleteWorkerHeartbeat(context.Context, string) error { return nil }
func (r *recordingStore) AcknowledgeIncident(context.Context, string, string) (models.Incident, error) {
	return models.Incident{}, nil
}
func (r *recordingStore) GetOrganization(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (r *recordingStore) GetOrganizationByClerkID(context.Context, string) (models.Organization, error) {
	return models.Organization{}, nil
}
func (r *recordingStore) UpsertOrganization(context.Context, models.Organization) (models.Organization, error) {
	return models.Organization{}, nil
}
func (r *recordingStore) DeleteOrganizationByClerkID(context.Context, string) error { return nil }
func (r *recordingStore) GetUserByID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (r *recordingStore) GetUserByClerkID(context.Context, string) (models.User, error) {
	return models.User{}, nil
}
func (r *recordingStore) UpsertUser(context.Context, models.User) (models.User, error) {
	return models.User{}, nil
}
func (r *recordingStore) DeleteUserByClerkID(context.Context, string) error { return nil }
func (r *recordingStore) ListMembershipsForUser(context.Context, string) ([]models.MembershipDetail, error) {
	return nil, nil
}
func (r *recordingStore) UpsertMembership(context.Context, models.Membership) error { return nil }
func (r *recordingStore) DeleteMembership(context.Context, string, string) error    { return nil }
func (r *recordingStore) RecordWebhookEvent(context.Context, string, string, []byte) (bool, error) {
	return true, nil
}
func (r *recordingStore) GetHeartbeat(context.Context, string) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (r *recordingStore) SetHeartbeat(context.Context, models.Heartbeat) (models.Heartbeat, error) {
	return models.Heartbeat{}, nil
}
func (r *recordingStore) DeleteHeartbeat(context.Context, string) error { return nil }
func (r *recordingStore) RecordHeartbeatPing(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (r *recordingStore) GetMultistepScript(context.Context, string) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (r *recordingStore) SetMultistepScript(context.Context, models.MultistepScript) (models.MultistepScript, error) {
	return models.MultistepScript{}, nil
}
func (r *recordingStore) GetBrowserScript(context.Context, string) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}
func (r *recordingStore) SetBrowserScript(context.Context, models.BrowserScript) (models.BrowserScript, error) {
	return models.BrowserScript{}, nil
}
func (r *recordingStore) EnqueueNotification(context.Context, models.OutboxEntry) (models.OutboxEntry, error) {
	return models.OutboxEntry{}, nil
}
func (r *recordingStore) ClaimPendingNotifications(context.Context, int) ([]models.OutboxEntry, pgx.Tx, error) {
	return nil, nil, nil
}
func (r *recordingStore) MarkNotificationDelivered(context.Context, pgx.Tx, string) error {
	return nil
}
func (r *recordingStore) MarkNotificationRetry(context.Context, pgx.Tx, string, int, int, string, time.Time) error {
	return nil
}
func (r *recordingStore) UpsertPushDevice(context.Context, models.PushDevice) (models.PushDevice, error) {
	return models.PushDevice{}, nil
}
func (r *recordingStore) DeletePushDevice(context.Context, string) error { return nil }
func (r *recordingStore) ListPushDevicesForOrg(context.Context, string) ([]models.PushDevice, error) {
	return nil, nil
}
func (r *recordingStore) ListPushDevicesForUser(context.Context, string) ([]models.PushDevice, error) {
	return nil, nil
}
func (r *recordingStore) ListStatusPages(context.Context) ([]models.StatusPage, error) { return nil, nil }
func (r *recordingStore) CreateStatusPage(context.Context, models.StatusPage) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (r *recordingStore) GetStatusPageBySlug(context.Context, string) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (r *recordingStore) GetStatusPageByDomain(context.Context, string) (models.StatusPage, error) {
	return models.StatusPage{}, nil
}
func (r *recordingStore) DeleteStatusPage(context.Context, string) error { return nil }
func (r *recordingStore) ListStatusPageComponents(context.Context, string) ([]models.StatusPageComponent, error) {
	return nil, nil
}
func (r *recordingStore) UpsertStatusPageComponent(context.Context, models.StatusPageComponent) (models.StatusPageComponent, error) {
	return models.StatusPageComponent{}, nil
}
func (r *recordingStore) DeleteStatusPageComponent(context.Context, string) error { return nil }
func (r *recordingStore) GetMonitorsByIDs(context.Context, string, []string) ([]models.Monitor, error) {
	return nil, nil
}
func (r *recordingStore) ListMaintenanceWindows(context.Context) ([]models.MaintenanceWindow, error) {
	return nil, nil
}
func (r *recordingStore) CreateMaintenanceWindow(context.Context, models.MaintenanceWindow) (models.MaintenanceWindow, error) {
	return models.MaintenanceWindow{}, nil
}
func (r *recordingStore) DeleteMaintenanceWindow(context.Context, string) error { return nil }
func (r *recordingStore) IsMonitorInMaintenance(context.Context, string, time.Time) (bool, error) {
	return false, nil
}
func (r *recordingStore) ListTags(context.Context) ([]models.Tag, error) { return nil, nil }
func (r *recordingStore) CreateTag(context.Context, models.Tag) (models.Tag, error) {
	return models.Tag{}, nil
}
func (r *recordingStore) DeleteTag(context.Context, string) error                { return nil }
func (r *recordingStore) SetMonitorTags(context.Context, string, []string) error { return nil }
func (r *recordingStore) ListMonitorsByTags(context.Context, []string) ([]models.Monitor, error) {
	return nil, nil
}
func (r *recordingStore) SLAReportForMonitor(context.Context, string, time.Time, time.Time) (models.SLAReport, error) {
	return models.SLAReport{}, nil
}
func (r *recordingStore) SLAReportForOrg(context.Context, time.Time, time.Time) (models.SLAReport, error) {
	return models.SLAReport{}, nil
}

func TestDeliverSignsAndRecordsSuccess(t *testing.T) {
	secret := "shhh-this-is-secret"
	var receivedSig atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedSig.Store(r.Header.Get("X-UpTime-Signature"))
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if r.Header.Get("X-UpTime-Signature") != expected {
			t.Errorf("bad signature: %q != %q", r.Header.Get("X-UpTime-Signature"), expected)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := &recordingStore{channels: []models.NotificationChannel{{
		ID: "c1", Name: "test", Type: "webhook", URL: server.URL, Enabled: true,
	}}}
	svc := NewService(store, Options{
		AllowPrivateTargets: true,
		SigningSecret:       secret,
		PerAttemptTimeout:   2 * time.Second,
	})
	svc.SendIncidentOpened(context.Background(), models.Monitor{ID: "m1", Name: "m"}, models.Incident{ID: "i1", Reason: "down"})

	if got, ok := receivedSig.Load().(string); !ok || !strings.HasPrefix(got, "sha256=") {
		t.Fatalf("expected signed delivery, got header %q", got)
	}
	if len(store.events) == 0 || !store.events[0].success {
		t.Fatalf("expected success event, got %+v", store.events)
	}
}

func TestDeliverRetriesOn5xxAndGivesUp(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store := &recordingStore{channels: []models.NotificationChannel{{
		ID: "c1", Name: "test", Type: "webhook", URL: server.URL, Enabled: true,
	}}}
	svc := NewService(store, Options{
		AllowPrivateTargets: true,
		PerAttemptTimeout:   500 * time.Millisecond,
		MaxRetries:          2,
	})
	svc.SendIncidentOpened(context.Background(), models.Monitor{ID: "m1"}, models.Incident{ID: "i1"})

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts (1 + 2 retries), got %d", got)
	}
	if len(store.events) == 0 || store.events[0].success {
		t.Fatalf("expected failure event, got %+v", store.events)
	}
}

func TestDeliverDoesNotRetry4xx(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	store := &recordingStore{channels: []models.NotificationChannel{{
		ID: "c1", Name: "test", Type: "webhook", URL: server.URL, Enabled: true,
	}}}
	svc := NewService(store, Options{
		AllowPrivateTargets: true,
		PerAttemptTimeout:   500 * time.Millisecond,
		MaxRetries:          5,
	})
	svc.SendIncidentResolved(context.Background(), models.Monitor{ID: "m1"}, models.Incident{ID: "i1"})

	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected exactly 1 attempt (no retry on 4xx), got %d", got)
	}
}

func TestEventJSONStable(t *testing.T) {
	event := IncidentEvent{Event: "incident.opened", IncidentID: "i", MonitorID: "m"}
	b, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	want := `"event":"incident.opened"`
	if !strings.Contains(string(b), want) {
		t.Fatalf("event JSON does not contain %s: %s", want, b)
	}
}
