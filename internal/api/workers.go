package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/models"
)

// workerStaleAfter is the grace period before a heartbeat is considered
// stale. Equal to ~3× the worker's heartbeat interval so a single missed
// write does not flip the UI to "down".
const workerStaleAfter = 20 * time.Second

// workersStatusResponse is the JSON payload behind /api/v1/workers/status.
// Aggregates are pre-computed so simple dashboards don't need to redo the
// math client-side, but the raw worker rows are also included for richer
// views.
type workersStatusResponse struct {
	GeneratedAt    time.Time                `json:"generatedAt"`
	StaleAfter     string                   `json:"staleAfter"`
	Workers        []models.WorkerHeartbeat `json:"workers"`
	RecentChecks   []models.CheckResult     `json:"recentChecks"`
	Aggregate      workersAggregate         `json:"aggregate"`
}

type workersAggregate struct {
	WorkerInstances int   `json:"workerInstances"`
	HealthyWorkers  int   `json:"healthyWorkers"`
	ActiveJobs      int   `json:"activeJobs"`
	QueueDepth      int   `json:"queueDepth"`
	QueueCapacity   int   `json:"queueCapacity"`
	JobsCompleted   int64 `json:"jobsCompleted"`
	JobsFailed      int64 `json:"jobsFailed"`
}

func (s *Server) workersStatus(c *gin.Context) {
	ctx := c.Request.Context()

	// Pull every heartbeat — even stale ones — so operators can see a
	// crashed instance in the UI rather than have it silently disappear.
	// Marking stale is the API's job, not the store's.
	heartbeats, err := s.store.ListWorkerHeartbeats(ctx, time.Time{})
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	now := time.Now().UTC()
	for i := range heartbeats {
		heartbeats[i].Stale = now.Sub(heartbeats[i].LastSeenAt) > workerStaleAfter
	}
	// Newest first, but always with non-stale workers ahead of stale ones
	// so the live workers are at the top of the table.
	sort.SliceStable(heartbeats, func(i, j int) bool {
		if heartbeats[i].Stale != heartbeats[j].Stale {
			return !heartbeats[i].Stale
		}
		return heartbeats[i].LastSeenAt.After(heartbeats[j].LastSeenAt)
	})

	recent, err := s.store.ListCheckResults(ctx, models.ResultFilter{Limit: 50})
	if err != nil {
		s.respond(c, nil, err)
		return
	}

	aggregate := workersAggregate{WorkerInstances: len(heartbeats)}
	for _, hb := range heartbeats {
		if hb.Stale {
			continue
		}
		aggregate.HealthyWorkers++
		aggregate.ActiveJobs += hb.ActiveJobs
		aggregate.QueueDepth += hb.QueueDepth
		aggregate.QueueCapacity += hb.QueueCapacity
		aggregate.JobsCompleted += hb.JobsCompleted
		aggregate.JobsFailed += hb.JobsFailed
	}

	c.JSON(http.StatusOK, workersStatusResponse{
		GeneratedAt:  now,
		StaleAfter:   workerStaleAfter.String(),
		Workers:      heartbeats,
		RecentChecks: recent,
		Aggregate:    aggregate,
	})
}
