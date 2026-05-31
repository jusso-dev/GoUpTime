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
	"github.com/jusso-dev/uptime/internal/repository"
)

type recordingStore struct {
	repository.NoopStore
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
