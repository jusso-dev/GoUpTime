package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
)

type fakeStore struct {
	repository.NoopStore
	mu    sync.Mutex
	beats []models.WorkerHeartbeat
	calls atomic.Int32
}

func (f *fakeStore) UpsertWorkerHeartbeat(_ context.Context, hb models.WorkerHeartbeat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls.Add(1)
	f.beats = append(f.beats, hb)
	return nil
}

func (f *fakeStore) latest() (models.WorkerHeartbeat, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.beats) == 0 {
		return models.WorkerHeartbeat{}, false
	}
	return f.beats[len(f.beats)-1], true
}

// sharedMetrics is built once because metrics.New uses promauto, which
// registers against the default registry — calling it twice would panic
// with "duplicate metrics collector registration attempted".
var sharedMetrics = metrics.New()

func newTestWorker(t *testing.T, store *fakeStore, workers int) *Worker {
	t.Helper()
	return New(
		config.Config{CheckWorkerCount: workers, Version: "test"},
		store,
		nil,
		sharedMetrics,
		slog.New(slog.NewTextHandler(testWriter{t: t}, nil)),
	)
}

func TestWriteHeartbeatPopulatesPoolState(t *testing.T) {
	store := &fakeStore{}
	w := newTestWorker(t, store, 4)
	w.activeJobs.Store(2)
	w.jobsCompleted.Store(10)
	w.jobsFailed.Store(1)
	w.inFlight.Store("mon-1", struct{}{})
	w.inFlight.Store("mon-2", struct{}{})

	if err := w.writeHeartbeat(context.Background()); err != nil {
		t.Fatalf("writeHeartbeat: %v", err)
	}
	hb, ok := store.latest()
	if !ok {
		t.Fatal("no heartbeat recorded")
	}
	if hb.InstanceID != w.instanceID {
		t.Fatalf("instance id mismatch: %q vs %q", hb.InstanceID, w.instanceID)
	}
	if hb.WorkerCount != 4 || hb.ActiveJobs != 2 || hb.JobsCompleted != 10 || hb.JobsFailed != 1 {
		t.Fatalf("unexpected counters: %+v", hb)
	}
	if len(hb.InFlight) != 2 {
		t.Fatalf("expected 2 in-flight, got %v", hb.InFlight)
	}
	if hb.QueueCapacity == 0 {
		t.Fatalf("expected non-zero queue capacity")
	}
}

func TestSnapshotInFlightIsConsistent(t *testing.T) {
	w := newTestWorker(t, &fakeStore{}, 1)
	for _, id := range []string{"a", "b", "c"} {
		w.inFlight.Store(id, struct{}{})
	}
	got := w.snapshotInFlight()
	if len(got) != 3 {
		t.Fatalf("expected 3 ids, got %v", got)
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !seen[want] {
			t.Fatalf("missing id %q in snapshot %v", want, got)
		}
	}
}

func TestHeartbeatLoopWritesAtLeastOnce(t *testing.T) {
	store := &fakeStore{}
	w := newTestWorker(t, store, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w.runHeartbeats(ctx)
	if store.calls.Load() < 1 {
		t.Fatalf("expected at least one heartbeat write")
	}
}

// testWriter routes log output to t.Log so the harness preserves it on
// failure but stays quiet on success.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
