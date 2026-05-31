// Browser / synthetic monitor. The actual Playwright execution lives in
// a separate Node sidecar (cmd/browser-worker, shipped in Phase 2b) that
// consumes from a Redis queue. This Go-side checker submits a job and
// waits for the result with a correlation id.
//
// Until the sidecar is configured (BrowserChecker.Enabled == false) the
// checker reports a clear, single-line error so operators can see at a
// glance that the monitor is unconfigured rather than broken.

package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/jusso-dev/uptime/internal/models"
)

// BrowserStore is the slice of repository.Store the checker depends on.
type BrowserStore interface {
	GetBrowserScript(ctx context.Context, monitorID string) (models.BrowserScript, error)
}

type BrowserChecker struct {
	Options Options
	Store   BrowserStore
	Redis   *redis.Client
	Enabled bool
}

type browserJob struct {
	JobID         string `json:"jobId"`
	MonitorID     string `json:"monitorId"`
	Source        string `json:"source"`
	TimeoutMS     int64  `json:"timeoutMs"`
	ResultChannel string `json:"resultChannel"`
}

type browserResult struct {
	JobID         string `json:"jobId"`
	Success       bool   `json:"success"`
	DurationMS    int64  `json:"durationMs"`
	ScreenshotURL string `json:"screenshotUrl"`
	Error         string `json:"error"`
}

const (
	browserQueueKey      = "queue:browser"
	browserResultChannel = "queue:browser:results"
)

func (c BrowserChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	if !c.Enabled || c.Redis == nil {
		result.Error = "browser checks disabled: set BROWSER_CHECK_ENABLED=true and run the browser-worker sidecar"
		return result, nil
	}
	if c.Store == nil {
		err := fmt.Errorf("browser checker is missing its store")
		result.Error = err.Error()
		return result, err
	}
	script, err := c.Store.GetBrowserScript(ctx, monitor.ID)
	if err != nil {
		result.Error = fmt.Sprintf("browser script missing: %v", err)
		return result, err
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	job := browserJob{
		JobID:         uuid.NewString(),
		MonitorID:     monitor.ID,
		Source:        script.Source,
		TimeoutMS:     timeout.Milliseconds(),
		ResultChannel: browserResultChannel,
	}
	payload, err := json.Marshal(job)
	if err != nil {
		result.Error = fmt.Sprintf("encode browser job: %v", err)
		return result, err
	}

	// Subscribe BEFORE pushing so we never miss the sidecar's response.
	pubsub := c.Redis.Subscribe(checkCtx, browserResultChannel)
	defer pubsub.Close()
	// Wait for the subscribe ack before publishing.
	if _, err := pubsub.Receive(checkCtx); err != nil {
		result.Error = fmt.Sprintf("subscribe to results: %v", err)
		return result, err
	}

	if err := c.Redis.LPush(checkCtx, browserQueueKey, payload).Err(); err != nil {
		result.Error = fmt.Sprintf("enqueue browser job: %v", err)
		return result, err
	}

	start := time.Now()
	ch := pubsub.Channel()
	for {
		select {
		case <-checkCtx.Done():
			result.Error = "browser check timed out waiting for sidecar"
			return result, checkCtx.Err()
		case msg, ok := <-ch:
			if !ok {
				return result, errors.New("browser result channel closed unexpectedly")
			}
			var r browserResult
			if err := json.Unmarshal([]byte(msg.Payload), &r); err != nil {
				continue
			}
			if r.JobID != job.JobID {
				// Another job's result; keep waiting.
				continue
			}
			result.CheckedAt = time.Now().UTC()
			result.ResponseTimeMS = r.DurationMS
			result.TotalMS = r.DurationMS
			if r.Error != "" {
				result.Error = r.Error
			}
			if r.Success {
				result.Success = true
				result.Status = models.StatusUp
				if r.ScreenshotURL != "" {
					result.ResponseSnippet = "screenshot:" + r.ScreenshotURL
				}
			}
			_ = start
			return result, nil
		}
	}
}
