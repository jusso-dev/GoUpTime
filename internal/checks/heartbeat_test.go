package checks

import (
	"context"
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type stubHeartbeatStore struct {
	hb  models.Heartbeat
	err error
}

func (s stubHeartbeatStore) GetHeartbeat(context.Context, string) (models.Heartbeat, error) {
	return s.hb, s.err
}

func TestHeartbeatUpWhenFreshPing(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	last := now.Add(-30 * time.Second)
	c := HeartbeatChecker{
		Store: stubHeartbeatStore{hb: models.Heartbeat{
			MonitorID:               "m1",
			ExpectedIntervalSeconds: 60,
			GraceSeconds:            10,
			LastPingAt:              &last,
		}},
		Now: func() time.Time { return now },
	}
	res, _ := c.Check(context.Background(), models.Monitor{ID: "m1"})
	if !res.Success || res.Status != models.StatusUp {
		t.Fatalf("expected up, got %+v", res)
	}
	if res.ResponseTimeMS != 30000 {
		t.Errorf("expected 30000ms since last ping, got %d", res.ResponseTimeMS)
	}
}

func TestHeartbeatDownWhenStale(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	last := now.Add(-5 * time.Minute)
	c := HeartbeatChecker{
		Store: stubHeartbeatStore{hb: models.Heartbeat{
			MonitorID:               "m1",
			ExpectedIntervalSeconds: 60,
			GraceSeconds:            10,
			LastPingAt:              &last,
		}},
		Now: func() time.Time { return now },
	}
	res, _ := c.Check(context.Background(), models.Monitor{ID: "m1"})
	if res.Success {
		t.Fatalf("expected failure for stale heartbeat, got %+v", res)
	}
	if res.Error == "" {
		t.Errorf("expected error message describing the miss")
	}
}

func TestHeartbeatTreatsFreshMonitorAsUpDuringSettle(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	c := HeartbeatChecker{
		Store: stubHeartbeatStore{hb: models.Heartbeat{
			MonitorID:               "m1",
			ExpectedIntervalSeconds: 60,
			GraceSeconds:            10,
			LastPingAt:              nil,
		}},
		Now: func() time.Time { return now },
	}
	monitor := models.Monitor{ID: "m1", CreatedAt: now.Add(-30 * time.Second)}
	res, _ := c.Check(context.Background(), monitor)
	if !res.Success {
		t.Fatalf("expected up during settle period, got %+v", res)
	}
}
