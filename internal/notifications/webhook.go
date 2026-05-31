// Package notifications delivers incident lifecycle events to user-supplied
// webhook URLs. Deliveries are bounded (timeout + retry budget) and signed
// when a shared secret is configured so receivers can authenticate payloads.
package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jusso-dev/uptime/internal/checks"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
)

// Options configures the notification service. Zero values get sensible
// production defaults via NewService.
type Options struct {
	AllowPrivateTargets bool
	// SigningSecret, if non-empty, HMAC-SHA256 signs each request body and
	// adds X-UpTime-Signature so receivers can verify authenticity.
	SigningSecret string
	// PerAttemptTimeout caps each individual webhook POST.
	PerAttemptTimeout time.Duration
	// MaxRetries is the number of *additional* attempts after the first
	// failure. Zero disables retries.
	MaxRetries int
	UserAgent  string
}

func (o Options) withDefaults() Options {
	if o.PerAttemptTimeout <= 0 {
		o.PerAttemptTimeout = 10 * time.Second
	}
	if o.MaxRetries < 0 {
		o.MaxRetries = 0
	}
	if o.UserAgent == "" {
		o.UserAgent = "UpTime-Notifier/1.0"
	}
	return o
}

type Service struct {
	store  repository.Store
	opts   Options
	client *http.Client
}

func NewService(store repository.Store, opts Options) *Service {
	opts = opts.withDefaults()
	return &Service{
		store:  store,
		opts:   opts,
		client: &http.Client{Timeout: opts.PerAttemptTimeout},
	}
}

// IncidentEvent is the JSON shape posted to webhook URLs. Fields are
// stable; new fields may be added but existing ones won't change meaning.
type IncidentEvent struct {
	Event       string    `json:"event"`
	IncidentID  string    `json:"incidentId"`
	MonitorID   string    `json:"monitorId"`
	MonitorName string    `json:"monitorName"`
	Status      string    `json:"status,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	Impact      string    `json:"impact,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	ResolvedAt  time.Time `json:"resolvedAt,omitempty"`
}

func (s *Service) SendIncidentOpened(ctx context.Context, monitor models.Monitor, incident models.Incident) {
	s.send(ctx, IncidentEvent{
		Event:       "incident.opened",
		IncidentID:  incident.ID,
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		Status:      string(models.StatusDown),
		Severity:    string(incident.Severity),
		Impact:      string(incident.Impact),
		Reason:      incident.Reason,
		StartedAt:   incident.StartedAt,
	})
}

func (s *Service) SendIncidentResolved(ctx context.Context, monitor models.Monitor, incident models.Incident) {
	resolvedAt := time.Now().UTC()
	if incident.ResolvedAt != nil {
		resolvedAt = *incident.ResolvedAt
	}
	s.send(ctx, IncidentEvent{
		Event:       "incident.resolved",
		IncidentID:  incident.ID,
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		Severity:    string(incident.Severity),
		Impact:      string(incident.Impact),
		ResolvedAt:  resolvedAt,
	})
}

func (s *Service) SendStatusPageAnnouncement(ctx context.Context, subscribers []models.StatusPageSubscriber, announcement models.StatusPageAnnouncement) {
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		return
	}
	for _, channel := range channels {
		if !channel.Enabled || (channel.Type != "smtp" && channel.Type != "email") {
			continue
		}
		for _, subscriber := range subscribers {
			if subscriber.ConfirmedAt == nil || !subscriberMatchesAnnouncement(subscriber, announcement) {
				continue
			}
			clone := channel
			cfg := map[string]any{}
			for k, v := range channel.Config {
				cfg[k] = v
			}
			cfg["to"] = subscriber.Email
			clone.Config = cfg
			s.deliverSMTP(ctx, clone, IncidentEvent{
				Event:       "status_page.announcement",
				IncidentID:  announcement.IncidentID,
				MonitorName: announcement.Title,
				Reason:      announcement.Body,
				StartedAt:   time.Now().UTC(),
			})
		}
	}
}

func subscriberMatchesAnnouncement(subscriber models.StatusPageSubscriber, announcement models.StatusPageAnnouncement) bool {
	if len(subscriber.ComponentIDs) == 0 || len(announcement.ComponentIDs) == 0 {
		return true
	}
	allowed := map[string]bool{}
	for _, id := range subscriber.ComponentIDs {
		allowed[id] = true
	}
	for _, id := range announcement.ComponentIDs {
		if allowed[id] {
			return true
		}
	}
	return false
}

func (s *Service) send(ctx context.Context, event IncidentEvent) {
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		// We've already returned to the caller (this is goroutined off);
		// best we can do is record the failure for ops.
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	// Fan out across channels concurrently. Sequential delivery would let a
	// single slow webhook stall every other receiver.
	var wg sync.WaitGroup
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		ch := channel
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch ch.Type {
			case "webhook":
				s.deliver(ctx, ch, event, payload)
			case "slack", "discord", "teams", "google_chat":
				s.deliver(ctx, ch, event, chatPayload(event, ch.Type))
			case "telegram":
				s.deliverTelegram(ctx, ch, event)
			case "smtp":
				s.deliverSMTP(ctx, ch, event)
			}
		}()
	}
	wg.Wait()
}

// deliver attempts to POST the payload to channel.URL up to 1+MaxRetries
// times with exponential backoff plus jitter. Every attempt is logged in
// notification_events so operators have an audit trail.
func (s *Service) deliver(ctx context.Context, channel models.NotificationChannel, event IncidentEvent, payload []byte) {
	if _, err := checks.ValidateURL(channel.URL, s.opts.AllowPrivateTargets); err != nil {
		_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, false, 0, fmt.Sprintf("blocked: %v", err))
		return
	}

	signature := s.sign(payload)
	deadline := time.Duration(s.opts.MaxRetries+1) * s.opts.PerAttemptTimeout
	if deadline <= 0 {
		deadline = s.opts.PerAttemptTimeout
	}
	overallCtx, cancel := context.WithTimeout(ctx, deadline*2)
	defer cancel()

	var lastStatus int
	var lastErrText string
	for attempt := 0; attempt <= s.opts.MaxRetries; attempt++ {
		if overallCtx.Err() != nil {
			lastErrText = "delivery context cancelled"
			break
		}
		status, errText, retryable := s.postOnce(overallCtx, channel.URL, payload, signature, event.Event, attempt)
		lastStatus, lastErrText = status, errText
		if errText == "" && status >= 200 && status < 300 {
			_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, true, status, "")
			return
		}
		if !retryable || attempt == s.opts.MaxRetries {
			break
		}
		// Exponential backoff with jitter: 250ms, 500ms, 1s, ... capped.
		backoff := time.Duration(250) * time.Millisecond * (1 << attempt)
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
		backoff += time.Duration(mathrand.Int64N(int64(backoff / 2)))
		select {
		case <-time.After(backoff):
		case <-overallCtx.Done():
			lastErrText = "delivery context cancelled"
			break
		}
	}
	_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, false, lastStatus, lastErrText)
}

// postOnce performs a single HTTP attempt. retryable is true for transport
// errors and 5xx/429 responses; 4xx responses are treated as terminal.
func (s *Service) postOnce(ctx context.Context, url string, payload []byte, signature, eventType string, attempt int) (status int, errText string, retryable bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Sprintf("build request: %v", err), false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.opts.UserAgent)
	req.Header.Set("X-UpTime-Event", eventType)
	req.Header.Set("X-UpTime-Delivery-Attempt", strconv.Itoa(attempt+1))
	if signature != "" {
		req.Header.Set("X-UpTime-Signature", "sha256="+signature)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return 0, "request timed out", true
		case errors.Is(err, context.Canceled):
			return 0, "request cancelled", false
		}
		return 0, err.Error(), true
	}
	defer resp.Body.Close()
	// Drain (and discard) the body so the connection can be reused. We cap
	// reads at 64 KiB to avoid pathological receivers.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp.StatusCode, "", false
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return resp.StatusCode, fmt.Sprintf("webhook returned status %d", resp.StatusCode), true
	default:
		return resp.StatusCode, fmt.Sprintf("webhook returned status %d", resp.StatusCode), false
	}
}

// sign returns the HMAC-SHA256 of payload as a hex string, or "" if no
// signing secret is configured.
func (s *Service) sign(payload []byte) string {
	if s.opts.SigningSecret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.opts.SigningSecret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func chatPayload(event IncidentEvent, channelType string) []byte {
	text := fmt.Sprintf("%s: %s", event.Event, event.MonitorName)
	if event.Reason != "" {
		text += " - " + event.Reason
	}
	switch channelType {
	case "discord":
		payload, _ := json.Marshal(map[string]string{"content": text})
		return payload
	default:
		payload, _ := json.Marshal(map[string]string{"text": text})
		return payload
	}
}

func (s *Service) deliverTelegram(ctx context.Context, channel models.NotificationChannel, event IncidentEvent) {
	token := strings.TrimSpace(stringFromConfig(channel.Config, "botToken"))
	chatID := strings.TrimSpace(stringFromConfig(channel.Config, "chatId"))
	if token == "" || chatID == "" {
		_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, false, 0, "telegram botToken and chatId are required")
		return
	}
	channel.URL = "https://api.telegram.org/bot" + token + "/sendMessage"
	payload, _ := json.Marshal(map[string]string{
		"chat_id": chatID,
		"text":    fmt.Sprintf("%s: %s %s", event.Event, event.MonitorName, event.Reason),
	})
	s.deliver(ctx, channel, event, payload)
}

func (s *Service) deliverSMTP(ctx context.Context, channel models.NotificationChannel, event IncidentEvent) {
	host := strings.TrimSpace(stringFromConfig(channel.Config, "host"))
	port := strings.TrimSpace(stringFromConfig(channel.Config, "port"))
	from := strings.TrimSpace(stringFromConfig(channel.Config, "from"))
	to := strings.TrimSpace(stringFromConfig(channel.Config, "to"))
	if port == "" {
		port = "587"
	}
	if host == "" || from == "" || to == "" {
		_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, false, 0, "smtp host, from, and to are required")
		return
	}
	addr := host + ":" + port
	subject := "UpTime " + event.Event + ": " + event.MonitorName
	body := fmt.Sprintf("%s\nMonitor: %s\nReason: %s\n", event.Event, event.MonitorName, event.Reason)
	msg := []byte("From: " + from + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body)
	var auth smtp.Auth
	username := stringFromConfig(channel.Config, "username")
	password := stringFromConfig(channel.Config, "password")
	if username != "" || password != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	sendCtx, cancel := context.WithTimeout(ctx, s.opts.PerAttemptTimeout)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- smtp.SendMail(addr, auth, from, splitRecipients(to), msg)
	}()
	select {
	case err := <-errCh:
		if err != nil {
			_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, false, 0, err.Error())
			return
		}
		_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, true, 250, "")
	case <-sendCtx.Done():
		_ = s.store.LogNotificationEvent(ctx, channel.ID, event.IncidentID, event.Event, false, 0, "smtp delivery timed out")
	}
}

func splitRecipients(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type WebhookProvider struct {
	svc *Service
}

func NewWebhookProvider(svc *Service) *WebhookProvider { return &WebhookProvider{svc: svc} }

func (p *WebhookProvider) Type() string { return "webhook" }

func (p *WebhookProvider) Send(ctx context.Context, channel models.NotificationChannel, event Event) (Delivery, error) {
	url := channel.URL
	if configured := stringFromConfig(channel.Config, "webhook_url"); configured != "" {
		url = configured
	}
	if url == "" {
		return Delivery{}, fmt.Errorf("webhook channel %s: url missing", channel.ID)
	}
	payload, err := json.Marshal(IncidentEvent{
		Event:       event.Type,
		IncidentID:  event.IncidentID,
		MonitorID:   event.MonitorID,
		MonitorName: event.MonitorName,
		Status:      event.Status,
		Reason:      event.Reason,
	})
	if err != nil {
		return Delivery{}, fmt.Errorf("encode webhook payload: %w", err)
	}
	status, errText, retryable := p.svc.postOnce(ctx, url, payload, p.svc.sign(payload), event.Type, 0)
	d := Delivery{StatusCode: status, Retryable: retryable}
	if errText != "" {
		return d, fmt.Errorf("%s", errText)
	}
	return d, nil
}
