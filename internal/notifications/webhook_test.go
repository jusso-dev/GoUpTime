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
