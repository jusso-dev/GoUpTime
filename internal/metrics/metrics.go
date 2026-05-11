package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/jusso-dev/uptime/internal/models"
)

type Metrics struct {
	ChecksTotal     *prometheus.CounterVec
	ChecksFailed    *prometheus.CounterVec
	CheckDuration   *prometheus.HistogramVec
	MonitorStatus   *prometheus.GaugeVec
	OpenIncidents   prometheus.Gauge
	WorkerActive    prometheus.Gauge
	WorkerCompleted prometheus.Counter
	WorkerFailed    prometheus.Counter
	APIRequests     *prometheus.CounterVec
	APIDuration     *prometheus.HistogramVec
	APIErrors       *prometheus.CounterVec
}

func New() *Metrics {
	return &Metrics{
		ChecksTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "uptime_checks_total",
			Help: "Total checks executed.",
		}, []string{"monitor_type", "status"}),
		ChecksFailed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "uptime_checks_failed_total",
			Help: "Total failed checks.",
		}, []string{"monitor_type"}),
		CheckDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "uptime_check_duration_seconds",
			Help:    "Check duration by monitor type.",
			Buckets: prometheus.DefBuckets,
		}, []string{"monitor_type"}),
		MonitorStatus: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "uptime_monitor_status",
			Help: "Monitor status where 1=up, 0=down, 0.5=degraded.",
		}, []string{"monitor_id"}),
		OpenIncidents: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "uptime_open_incidents",
			Help: "Open incident count.",
		}),
		WorkerActive: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "uptime_worker_jobs_active",
			Help: "Active worker jobs.",
		}),
		WorkerCompleted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "uptime_worker_jobs_completed_total",
			Help: "Completed worker jobs.",
		}),
		WorkerFailed: promauto.NewCounter(prometheus.CounterOpts{
			Name: "uptime_worker_jobs_failed_total",
			Help: "Failed worker jobs.",
		}),
		APIRequests: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "uptime_api_requests_total",
			Help: "API request count.",
		}, []string{"method", "path", "status"}),
		APIDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "uptime_api_request_duration_seconds",
			Help:    "API request duration.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),
		APIErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "uptime_api_errors_total",
			Help: "API error count.",
		}, []string{"method", "path"}),
	}
}

func (m *Metrics) ObserveCheck(monitor models.Monitor, result models.CheckResult) {
	m.ChecksTotal.WithLabelValues(string(monitor.Type), string(result.Status)).Inc()
	m.CheckDuration.WithLabelValues(string(monitor.Type)).Observe(float64(result.TotalMS) / 1000)
	if !result.Success {
		m.ChecksFailed.WithLabelValues(string(monitor.Type)).Inc()
	}
	statusValue := 0.0
	if result.Status == models.StatusUp {
		statusValue = 1
	} else if result.Status == models.StatusDegraded {
		statusValue = 0.5
	}
	if monitor.ID != "" {
		m.MonitorStatus.WithLabelValues(monitor.ID).Set(statusValue)
	}
}

func (m *Metrics) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		status := strconv.Itoa(c.Writer.Status())
		m.APIRequests.WithLabelValues(c.Request.Method, path, status).Inc()
		m.APIDuration.WithLabelValues(c.Request.Method, path).Observe(time.Since(start).Seconds())
		if c.Writer.Status() >= 500 {
			m.APIErrors.WithLabelValues(c.Request.Method, path).Inc()
		}
	}
}
