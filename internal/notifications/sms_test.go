package notifications

import (
	"strings"
	"testing"
)

func TestSMSBodyIncludesSeverityAndTruncates(t *testing.T) {
	body := smsBody(Event{
		Type:        "incident.opened",
		MonitorID:   "m1",
		MonitorName: "API",
		Severity:    "critical",
		Reason:      strings.Repeat("down ", 100),
		URL:         "https://example.com/incidents/i1",
	})
	if !strings.HasPrefix(body, "[critical] API incident:") {
		t.Fatalf("unexpected sms body: %q", body)
	}
	if len(body) > 320 {
		t.Fatalf("sms body should be capped, got %d chars", len(body))
	}
}
