package checks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

func TestHTTPCheckerSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	checker := HTTPChecker{Options: Options{AllowPrivateTargets: true, DefaultTimeout: time.Second}}
	result, err := checker.Check(context.Background(), models.Monitor{Type: models.MonitorHTTP, Target: server.URL, Method: "GET", ExpectedStatus: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Status != models.StatusUp || result.StatusCode != 200 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.TotalMS < 0 {
		t.Fatal("expected non-negative timing")
	}
}

func TestHTTPCheckerTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := HTTPChecker{Options: Options{AllowPrivateTargets: true, DefaultTimeout: time.Second}}
	result, err := checker.Check(context.Background(), models.Monitor{Type: models.MonitorHTTP, Target: server.URL, TimeoutSeconds: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success with default timeout: %+v", result)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	result, err = checker.Check(ctx, models.Monitor{Type: models.MonitorHTTP, Target: server.URL, TimeoutSeconds: 1})
	if err == nil || result.Success {
		t.Fatalf("expected timeout failure, result=%+v err=%v", result, err)
	}
}

func TestHTTPCheckerExpectedStatusMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	checker := HTTPChecker{Options: Options{AllowPrivateTargets: true, DefaultTimeout: time.Second}}
	result, err := checker.Check(context.Background(), models.Monitor{Type: models.MonitorHTTP, Target: server.URL, ExpectedStatus: 200})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.Status != models.StatusDown {
		t.Fatalf("expected down result: %+v", result)
	}
}

func TestAPICheckerJSONAssertions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"count":3,"items":[{"name":"alpha"}]}`))
	}))
	defer server.Close()

	checker := HTTPChecker{Options: Options{AllowPrivateTargets: true, DefaultTimeout: time.Second}}
	result, err := checker.Check(context.Background(), models.Monitor{
		Type:           models.MonitorAPI,
		Target:         server.URL,
		Method:         "POST",
		ExpectedStatus: http.StatusCreated,
		Config: map[string]any{
			"bearerToken": "secret",
			"body":        map[string]any{"probe": true},
			"assertions": []map[string]any{
				{"path": "$.ok", "operator": "equals", "value": true},
				{"path": "$.count", "operator": "greaterThan", "value": 2},
				{"path": "$.items[0].name", "operator": "contains", "value": "alp"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected API assertion success: %+v", result)
	}
}

func TestValidateURLBlocksLocalhost(t *testing.T) {
	_, err := ValidateURL("http://localhost:8080", false)
	if !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("expected blocked target, got %v", err)
	}
}
