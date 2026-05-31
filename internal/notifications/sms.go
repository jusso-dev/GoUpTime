package notifications

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type TwilioProvider struct {
	kind      string
	client    *http.Client
	userAgent string
}

func NewTwilioProvider(kind, userAgent string, timeout time.Duration) *TwilioProvider {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if userAgent == "" {
		userAgent = "UpTime-Notifier/1.0"
	}
	return &TwilioProvider{kind: kind, client: &http.Client{Timeout: timeout}, userAgent: userAgent}
}

func (p *TwilioProvider) Type() string { return p.kind }

func (p *TwilioProvider) Send(ctx context.Context, channel models.NotificationChannel, event Event) (Delivery, error) {
	accountSID := stringFromConfig(channel.Config, "account_sid")
	authToken := stringFromConfig(channel.Config, "auth_token")
	from := stringFromConfig(channel.Config, "from")
	to := stringFromConfig(channel.Config, "to")
	if accountSID == "" || authToken == "" || from == "" || to == "" {
		return Delivery{}, fmt.Errorf("twilio channel %s: account_sid, auth_token, from, and to are required", channel.ID)
	}
	endpoint := "Messages.json"
	values := url.Values{"From": {from}, "To": {to}}
	if p.kind == "twilio_voice" {
		endpoint = "Calls.json"
		callback := stringFromConfig(channel.Config, "twiml_url")
		if callback == "" {
			callback = stringFromConfig(channel.Config, "url")
		}
		if callback == "" {
			return Delivery{}, fmt.Errorf("twilio voice channel %s: twiml_url is required", channel.ID)
		}
		values.Set("Url", callback)
	} else {
		values.Set("Body", smsBody(event))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.twilio.com/2010-04-01/Accounts/"+url.PathEscape(accountSID)+"/"+endpoint,
		strings.NewReader(values.Encode()))
	if err != nil {
		return Delivery{}, err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(accountSID+":"+authToken)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return Delivery{Retryable: true}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return deliveryFromStatus(resp.StatusCode, "twilio")
}

type SNSSMSProvider struct {
	client    *http.Client
	userAgent string
}

func NewSNSSMSProvider(userAgent string, timeout time.Duration) *SNSSMSProvider {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if userAgent == "" {
		userAgent = "UpTime-Notifier/1.0"
	}
	return &SNSSMSProvider{client: &http.Client{Timeout: timeout}, userAgent: userAgent}
}

func (p *SNSSMSProvider) Type() string { return "aws_sns_sms" }

func (p *SNSSMSProvider) Send(ctx context.Context, channel models.NotificationChannel, event Event) (Delivery, error) {
	endpoint := stringFromConfig(channel.Config, "endpoint")
	if endpoint == "" {
		endpoint = channel.URL
	}
	phone := stringFromConfig(channel.Config, "phone_number")
	if endpoint == "" || phone == "" {
		return Delivery{}, fmt.Errorf("sns sms channel %s: endpoint and phone_number are required", channel.ID)
	}
	payload, _ := json.Marshal(map[string]any{
		"phoneNumber": phone,
		"message":     smsBody(event),
		"event":       event,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Delivery{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return Delivery{Retryable: true}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return deliveryFromStatus(resp.StatusCode, "sns")
}

func smsBody(event Event) string {
	name := event.MonitorName
	if name == "" {
		name = event.MonitorID
	}
	state := "incident"
	if event.Type == "incident.resolved" {
		state = "resolved"
	}
	body := fmt.Sprintf("%s %s: %s", name, state, event.Reason)
	if event.Severity != "" {
		body = "[" + event.Severity + "] " + body
	}
	if event.URL != "" {
		body += " " + event.URL
	}
	if len(body) > 320 {
		body = body[:317] + "..."
	}
	return body
}

func deliveryFromStatus(status int, provider string) (Delivery, error) {
	d := Delivery{StatusCode: status}
	switch {
	case status >= 200 && status < 300:
		return d, nil
	case status == http.StatusTooManyRequests || status >= 500:
		d.Retryable = true
		return d, fmt.Errorf("%s returned status %d", provider, status)
	default:
		return d, fmt.Errorf("%s returned status %d", provider, status)
	}
}
