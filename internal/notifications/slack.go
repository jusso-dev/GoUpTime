// Slack provider. Posts incident events to a per-channel incoming-webhook
// URL using Block Kit so the message renders well in both the desktop
// and mobile Slack apps without requiring a full Slack app review.

package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type SlackProvider struct {
	client    *http.Client
	userAgent string
}

func NewSlackProvider(userAgent string, timeout time.Duration) *SlackProvider {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if userAgent == "" {
		userAgent = "UpTime-Notifier/1.0"
	}
	return &SlackProvider{
		client:    &http.Client{Timeout: timeout},
		userAgent: userAgent,
	}
}

func (p *SlackProvider) Type() string { return "slack" }

func (p *SlackProvider) Send(ctx context.Context, channel models.NotificationChannel, event Event) (Delivery, error) {
	webhookURL := stringFromConfig(channel.Config, "webhook_url")
	if webhookURL == "" {
		// Tolerate legacy rows that stored the URL in the top-level column.
		webhookURL = channel.URL
	}
	if webhookURL == "" {
		return Delivery{}, fmt.Errorf("slack channel %s: webhook_url missing from config", channel.ID)
	}

	payload, err := json.Marshal(slackBody(channel, event))
	if err != nil {
		return Delivery{}, fmt.Errorf("encode slack payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return Delivery{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return Delivery{StatusCode: 0, Retryable: true}, fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	d := Delivery{StatusCode: resp.StatusCode}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return d, nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		d.Retryable = true
		return d, fmt.Errorf("slack returned status %d", resp.StatusCode)
	default:
		return d, fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
}

func slackBody(channel models.NotificationChannel, event Event) map[string]any {
	headerText := fmt.Sprintf("🚨 %s is DOWN", event.MonitorName)
	color := "danger"
	if event.Type == "incident.resolved" {
		headerText = fmt.Sprintf("✅ %s recovered", event.MonitorName)
		color = "good"
	} else if event.Type == "incident.acknowledged" {
		headerText = fmt.Sprintf("👁 %s acknowledged", event.MonitorName)
		color = "warning"
	}

	fields := []map[string]any{
		{"type": "mrkdwn", "text": fmt.Sprintf("*Status*\n%s", event.Status)},
	}
	if event.Region != "" {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Region*\n%s", event.Region)})
	}
	if event.Reason != "" {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Reason*\n%s", event.Reason)})
	}
	if event.StartedAt != "" {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Started*\n%s", event.StartedAt)})
	}

	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]any{"type": "plain_text", "text": headerText, "emoji": true},
		},
		{
			"type":   "section",
			"fields": fields,
		},
	}
	if event.URL != "" {
		blocks = append(blocks, map[string]any{
			"type": "actions",
			"elements": []map[string]any{{
				"type":  "button",
				"text":  map[string]any{"type": "plain_text", "text": "View incident"},
				"url":   event.URL,
				"style": "primary",
			}},
		})
	}

	body := map[string]any{
		"text":   headerText,
		"blocks": blocks,
		"attachments": []map[string]any{{"color": color}},
	}
	if override := stringFromConfig(channel.Config, "channel_override"); override != "" {
		body["channel"] = override
	}
	if username := stringFromConfig(channel.Config, "username"); username != "" {
		body["username"] = username
	}
	return body
}

func stringFromConfig(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}
