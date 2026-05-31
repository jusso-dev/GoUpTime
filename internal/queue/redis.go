// Package queue holds the Redis-backed primitives the multi-region
// scheduler uses to fan jobs out to per-region workers and to aggregate
// the resulting per-region verdicts. Keeping the keys/payload shapes in
// one place makes it cheap to evolve the on-the-wire format later.

package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key namespaces. Workers BRPOP from queue:checks:{region}; the scheduler
// LPUSHes; aggregators SUBSCRIBE to queue:results.
const (
	CheckQueuePrefix     = "queue:checks:"
	ResultsChannel       = "queue:results"
	SchedulerLeaderLock  = "lock:scheduler"
	RegionResultsHashKey = "region_results:"
)

// Job is the payload pushed to queue:checks:{region}. The monitor snapshot
// is embedded so workers don't have to round-trip to Postgres for every
// job — the scheduler already loaded it.
type Job struct {
	JobID        string         `json:"jobId"`
	MonitorID    string         `json:"monitorId"`
	Region       string         `json:"region"`
	DispatchedAt time.Time      `json:"dispatchedAt"`
	Monitor      map[string]any `json:"monitor"`
}

// Result is the payload published to queue:results when a worker
// completes a job. The scheduler/aggregator listens on the channel and
// folds the verdict into the rolling per-monitor window.
type Result struct {
	JobID          string    `json:"jobId"`
	MonitorID      string    `json:"monitorId"`
	OrganizationID string    `json:"organizationId"`
	Region         string    `json:"region"`
	Success        bool      `json:"success"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	CheckedAt      time.Time `json:"checkedAt"`
}

// Client wraps redis.Client with type-safe push/pop helpers. The
// underlying client may be nil, in which case operations return
// ErrUnavailable so the caller can fall back to in-process scheduling.
type Client struct {
	r *redis.Client
}

// ErrUnavailable means Redis is not configured for this process; the
// caller should fall back to its non-distributed code path.
var ErrUnavailable = errors.New("queue: redis not configured")

func New(client *redis.Client) *Client {
	return &Client{r: client}
}

func (c *Client) Available() bool {
	return c != nil && c.r != nil
}

// EnqueueCheck pushes a job onto the per-region queue. Callers should
// retry transient failures; ErrUnavailable indicates a permanent local
// fallback rather than a retryable error.
func (c *Client) EnqueueCheck(ctx context.Context, region string, job Job) error {
	if !c.Available() {
		return ErrUnavailable
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode job: %w", err)
	}
	key := CheckQueuePrefix + region
	if err := c.r.LPush(ctx, key, payload).Err(); err != nil {
		return fmt.Errorf("lpush %s: %w", key, err)
	}
	return nil
}

// DequeueCheck blocks for up to timeout on the per-region queue and
// decodes the next job. Returns nil, nil on timeout (no job arrived).
func (c *Client) DequeueCheck(ctx context.Context, region string, timeout time.Duration) (*Job, error) {
	if !c.Available() {
		return nil, ErrUnavailable
	}
	key := CheckQueuePrefix + region
	res, err := c.r.BRPop(ctx, timeout, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("brpop %s: %w", key, err)
	}
	if len(res) < 2 {
		return nil, nil
	}
	var job Job
	if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
		return nil, fmt.Errorf("decode job: %w", err)
	}
	return &job, nil
}

// PublishResult fans a check verdict out to every subscriber on
// queue:results. Used by workers after they record a check_result row.
func (c *Client) PublishResult(ctx context.Context, r Result) error {
	if !c.Available() {
		return ErrUnavailable
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	if err := c.r.Publish(ctx, ResultsChannel, payload).Err(); err != nil {
		return fmt.Errorf("publish %s: %w", ResultsChannel, err)
	}
	return nil
}

// SubscribeResults returns a channel that yields every published Result.
// The caller is responsible for closing the returned PubSub when done.
func (c *Client) SubscribeResults(ctx context.Context) (*redis.PubSub, <-chan Result, error) {
	if !c.Available() {
		return nil, nil, ErrUnavailable
	}
	ps := c.r.Subscribe(ctx, ResultsChannel)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, nil, err
	}
	out := make(chan Result, 64)
	go func() {
		defer close(out)
		for msg := range ps.Channel() {
			var r Result
			if err := json.Unmarshal([]byte(msg.Payload), &r); err != nil {
				continue
			}
			out <- r
		}
	}()
	return ps, out, nil
}

// AcquireLeader attempts to claim the scheduler lock for ttl; returns
// (true, nil) if this caller is the leader. The same caller can refresh
// the lock by calling RefreshLeader with the same instance id.
func (c *Client) AcquireLeader(ctx context.Context, instanceID string, ttl time.Duration) (bool, error) {
	if !c.Available() {
		return false, ErrUnavailable
	}
	ok, err := c.r.SetNX(ctx, SchedulerLeaderLock, instanceID, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("setnx %s: %w", SchedulerLeaderLock, err)
	}
	return ok, nil
}

// RefreshLeader extends the lock TTL if and only if this instance still
// holds it. Uses a single-shot CAS-style Lua so an expired-and-reacquired
// lock isn't accidentally pushed forward by the prior leader.
func (c *Client) RefreshLeader(ctx context.Context, instanceID string, ttl time.Duration) (bool, error) {
	if !c.Available() {
		return false, ErrUnavailable
	}
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
  return 0
end`
	res, err := c.r.Eval(ctx, script, []string{SchedulerLeaderLock}, instanceID, ttl.Milliseconds()).Result()
	if err != nil {
		return false, fmt.Errorf("refresh leader: %w", err)
	}
	n, _ := res.(int64)
	return n == 1, nil
}

// ReleaseLeader deletes the lock if and only if this instance owns it.
// Safe to call on shutdown so a successor can take over immediately.
func (c *Client) ReleaseLeader(ctx context.Context, instanceID string) error {
	if !c.Available() {
		return nil
	}
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
else
  return 0
end`
	if _, err := c.r.Eval(ctx, script, []string{SchedulerLeaderLock}, instanceID).Result(); err != nil {
		return fmt.Errorf("release leader: %w", err)
	}
	return nil
}
