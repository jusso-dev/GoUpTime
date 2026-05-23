// Push provider. Sends incident events to every Expo push token
// registered against the channel's organization. Using Expo (instead
// of direct APNs/FCM) sidesteps cert management — the mobile client
// registers an expo_token and we just POST to exp.host.

package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
)

const expoPushURL = "https://exp.host/--/api/v2/push/send"

type PushProvider struct {
	store     repository.Store
	client    *http.Client
	userAgent string
	token     string // EXPO_ACCESS_TOKEN; optional
}

func NewPushProvider(store repository.Store, userAgent, accessToken string, timeout time.Duration) *PushProvider {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if userAgent == "" {
		userAgent = "UpTime-Notifier/1.0"
	}
	return &PushProvider{
		store:     store,
		client:    &http.Client{Timeout: timeout},
		userAgent: userAgent,
		token:     accessToken,
	}
}

func (p *PushProvider) Type() string { return "push" }

func (p *PushProvider) Send(ctx context.Context, channel models.NotificationChannel, event Event) (Delivery, error) {
	// Resolve all push devices for the org. The channel itself is just a
	// "fan out to everyone" sink; we don't store per-user device targets
	// on the channel.
	sysCtx := auth.WithSystemOrg(ctx, channel.OrganizationID)
	devices, err := p.store.ListPushDevicesForOrg(sysCtx, channel.OrganizationID)
	if err != nil {
		return Delivery{}, fmt.Errorf("list push devices: %w", err)
	}
	if len(devices) == 0 {
		// Nothing to do; not an error — the org just has no installs yet.
		return Delivery{StatusCode: http.StatusOK}, nil
	}

	title, body := pushTitleAndBody(event)
	messages := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		messages = append(messages, map[string]any{
			"to":    d.ExpoToken,
			"title": title,
			"body":  body,
			"sound": "default",
			"data": map[string]any{
				"incidentId": event.IncidentID,
				"monitorId":  event.MonitorID,
				"eventType":  event.Type,
				"url":        event.URL,
			},
		})
	}

	payload, err := json.Marshal(messages)
	if err != nil {
		return Delivery{}, fmt.Errorf("encode push payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, expoPushURL, bytes.NewReader(payload))
	if err != nil {
		return Delivery{}, fmt.Errorf("build expo request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("User-Agent", p.userAgent)
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return Delivery{Retryable: true}, fmt.Errorf("expo post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	d := Delivery{StatusCode: resp.StatusCode}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return d, nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		d.Retryable = true
		return d, fmt.Errorf("expo returned status %d", resp.StatusCode)
	default:
		return d, fmt.Errorf("expo returned status %d", resp.StatusCode)
	}
}

func pushTitleAndBody(event Event) (string, string) {
	switch event.Type {
	case "incident.resolved":
		return "Recovered: " + event.MonitorName, event.Status
	case "incident.acknowledged":
		return "Acknowledged: " + event.MonitorName, event.Reason
	default:
		return "Down: " + event.MonitorName, event.Reason
	}
}
