package checks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jusso-dev/uptime/internal/models"
)

type stubMultistepStore struct{ script models.MultistepScript }

func (s stubMultistepStore) GetMultistepScript(context.Context, string) (models.MultistepScript, error) {
	return s.script, nil
}

func TestMultistepLoginThenFetch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "secret123"})
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret123" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"email": "alice@example.com"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	exists := true
	c := MultistepChecker{
		Options: Options{DefaultTimeout: 3000000000},
		Store: stubMultistepStore{script: models.MultistepScript{
			Steps: models.MultistepSteps{
				Steps: []models.MultistepStep{
					{
						Name: "login", Method: "POST", URL: srv.URL + "/login",
						Assertions: []models.MultistepAssertion{
							{Status: 200},
							{JSONPath: "$.token", Exists: &exists},
						},
						Extract: map[string]string{"TOKEN": "$.token"},
					},
					{
						Name: "fetch-me", Method: "GET", URL: srv.URL + "/me",
						Headers: map[string]string{"Authorization": "Bearer {{.TOKEN}}"},
						Assertions: []models.MultistepAssertion{
							{Status: 200},
							{JSONPath: "$.email", Equals: "alice@example.com"},
						},
					},
				},
			},
		}},
	}
	res, _ := c.Check(context.Background(), models.Monitor{ID: "m1"})
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
}

func TestMultistepFailsOnAssertion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error"})
	}))
	defer srv.Close()

	c := MultistepChecker{
		Store: stubMultistepStore{script: models.MultistepScript{
			Steps: models.MultistepSteps{
				Steps: []models.MultistepStep{{
					Method: "GET", URL: srv.URL,
					Assertions: []models.MultistepAssertion{
						{JSONPath: "$.status", Equals: "ok"},
					},
				}},
			},
		}},
	}
	res, _ := c.Check(context.Background(), models.Monitor{ID: "m1"})
	if res.Success {
		t.Fatalf("expected failure, got %+v", res)
	}
}
