package checks

import (
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

func TestTLSExpiryStatus(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		notAfter time.Time
		want     models.CheckStatus
	}{
		{name: "expired", notAfter: now.Add(-time.Hour), want: models.StatusDown},
		{name: "near expiry", notAfter: now.Add(48 * time.Hour), want: models.StatusDegraded},
		{name: "healthy", notAfter: now.Add(30 * 24 * time.Hour), want: models.StatusUp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TLSExpiryStatus(tt.notAfter, 14, now); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}
