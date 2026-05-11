package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
)

type Service struct {
	store               repository.Store
	allowPrivateTargets bool
	client              *http.Client
}

func NewService(store repository.Store, allowPrivateTargets bool) *Service {
	return &Service{
		store:               store,
		allowPrivateTargets: allowPrivateTargets,
		client:              &http.Client{Timeout: 5 * time.Second},
	}
}

type IncidentEvent struct {
	Event       string    `json:"event"`
	MonitorID   string    `json:"monitorId"`
	MonitorName string    `json:"monitorName"`
	Status      string    `json:"status,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	ResolvedAt  time.Time `json:"resolvedAt,omitempty"`
}

func (s *Service) SendIncidentOpened(ctx context.Context, monitor models.Monitor, incident models.Incident) {
	s.send(ctx, incident.ID, IncidentEvent{
		Event:       "incident.opened",
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		Status:      string(models.StatusDown),
		Reason:      incident.Reason,
		StartedAt:   incident.StartedAt,
	})
}

func (s *Service) SendIncidentResolved(ctx context.Context, monitor models.Monitor, incident models.Incident) {
	resolvedAt := time.Now().UTC()
	if incident.ResolvedAt != nil {
		resolvedAt = *incident.ResolvedAt
	}
	s.send(ctx, incident.ID, IncidentEvent{
		Event:       "incident.resolved",
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		ResolvedAt:  resolvedAt,
	})
}

func (s *Service) send(ctx context.Context, incidentID string, event IncidentEvent) {
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		return
	}
	payload, _ := json.Marshal(event)
	for _, channel := range channels {
		if !channel.Enabled || channel.Type != "webhook" {
			continue
		}
		statusCode, errText := 0, ""
		success := false
		if _, err := checks.ValidateURL(channel.URL, s.allowPrivateTargets); err != nil {
			errText = err.Error()
		} else {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.URL, bytes.NewReader(payload))
			if err != nil {
				errText = err.Error()
			} else {
				req.Header.Set("Content-Type", "application/json")
				resp, err := s.client.Do(req)
				if err != nil {
					errText = err.Error()
				} else {
					statusCode = resp.StatusCode
					_ = resp.Body.Close()
					success = statusCode >= 200 && statusCode < 300
					if !success {
						errText = fmt.Sprintf("webhook returned status %d", statusCode)
					}
				}
			}
		}
		_ = s.store.LogNotificationEvent(ctx, channel.ID, incidentID, event.Event, success, statusCode, errText)
	}
}
