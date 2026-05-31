// Heartbeat (push) monitor. Unlike every other check type this one does
// not initiate a network call — it reads the heartbeats table and decides
// "did a ping arrive in time?" based on the last_ping_at column the
// public /api/v1/heartbeats/:token/ping endpoint maintains.

package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

// HeartbeatStore is the slice of the repository.Store interface this
// checker depends on. Keeping it narrow makes unit testing trivial and
// avoids an import cycle with the parent repository package.
type HeartbeatStore interface {
	GetHeartbeat(ctx context.Context, monitorID string) (models.Heartbeat, error)
}

type HeartbeatChecker struct {
	Options Options
	Store   HeartbeatStore
	// Now is overridable for time-based tests.
	Now func() time.Time
}

func (c HeartbeatChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	if c.Store == nil {
		err := fmt.Errorf("heartbeat checker is missing its store")
		result.Error = err.Error()
		return result, err
	}
	hb, err := c.Store.GetHeartbeat(ctx, monitor.ID)
	if err != nil {
		result.Error = fmt.Sprintf("heartbeat config missing: %v", err)
		return result, err
	}

	now := c.now()
	result.CheckedAt = now.UTC()

	if hb.LastPingAt == nil {
		// Never pinged: treat as down once the initial window plus grace
		// elapses since the monitor was created. Until then, report up to
		// avoid spurious incidents during onboarding.
		settleDeadline := monitor.CreatedAt.Add(time.Duration(hb.ExpectedIntervalSeconds+hb.GraceSeconds) * time.Second)
		if monitor.CreatedAt.IsZero() || now.Before(settleDeadline) {
			result.Success = true
			result.Status = models.StatusUp
			result.ResponseTimeMS = 0
			result.TotalMS = 0
			return result, nil
		}
		result.Error = "no heartbeat ping has been received yet"
		return result, nil
	}

	since := now.Sub(*hb.LastPingAt)
	deadline := time.Duration(hb.ExpectedIntervalSeconds+hb.GraceSeconds) * time.Second
	result.ResponseTimeMS = since.Milliseconds()
	result.TotalMS = since.Milliseconds()

	if since > deadline {
		result.Error = fmt.Sprintf("last heartbeat was %s ago; deadline was %s",
			since.Round(time.Second), deadline)
		return result, nil
	}
	result.Success = true
	result.Status = models.StatusUp
	return result, nil
}

func (c HeartbeatChecker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
