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

func TestValidateURLBlocksLocalhost(t *testing.T) {
	_, err := ValidateURL("http://localhost:8080", false)
	if !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("expected blocked target, got %v", err)
	}
}
