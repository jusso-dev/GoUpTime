// Package api wires the HTTP layer: routing, authentication, request
// binding, error mapping, and observability middleware. Handlers stay thin
// and delegate work to internal/service and internal/repository.
package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/jusso-dev/uptime/internal/api/web"
	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
)

// requestIDKey is the gin context key for the request correlation id. The
// string value matches the X-Request-ID response header so logs and traces
// line up with what clients see.
const requestIDKey = "requestID"

// Server holds the dependencies required by HTTP handlers. It is constructed
// once at startup and is safe for concurrent use because every field is
// either immutable (cfg) or itself safe for concurrent use.
type Server struct {
	cfg     config.Config
	store   repository.Store
	redis   *redis.Client
	monitor *service.MonitoringService
	metrics *metrics.Metrics
	logger  *slog.Logger
}

func NewRouter(cfg config.Config, store repository.Store, redisClient *redis.Client, monitor *service.MonitoringService, m *metrics.Metrics, logger *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	s := &Server{cfg: cfg, store: store, redis: redisClient, monitor: monitor, metrics: m, logger: logger}
	r := gin.New()

	// Middleware order matters: requestID first so every other middleware
	// (including recovery, which logs) has a correlation id to attach.
	r.Use(s.requestID(), s.recovery(), s.bodyLimit(), s.logging(), m.GinMiddleware())

	r.GET("/health", s.health)
	r.GET("/health-check", s.health)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.POST("/ping-endpoint", s.pingEndpoint)
	// Operator-facing dashboard. The HTML is unauthenticated; it prompts
	// the user for an API key client-side and uses it for the protected
	// /api/v1/workers/status XHR. This matches the rest of the API.
	r.GET("/workers", s.workersDashboard)
	r.NoRoute(s.notFound)
	r.NoMethod(s.methodNotAllowed)

	v1 := r.Group("/api/v1")
	v1.Use(s.apiKeyAuth())
	v1.POST("/check", s.manualCheck)
	v1.GET("/monitors", s.listMonitors)
	v1.POST("/monitors", s.createMonitor)
	v1.GET("/monitors/:id", s.getMonitor)
	v1.PUT("/monitors/:id", s.updateMonitor)
	v1.DELETE("/monitors/:id", s.deleteMonitor)
	v1.POST("/monitors/:id/check-now", s.checkNow)
	v1.GET("/monitors/:id/results", s.monitorResults)
	v1.GET("/check-results", s.checkResults)
	v1.GET("/incidents", s.listIncidents)
	v1.GET("/incidents/:id", s.getIncident)
	v1.POST("/incidents/:id/resolve", s.resolveIncident)
	v1.GET("/stats/overview", s.overviewStats)
	v1.GET("/stats/monitors/:id", s.monitorStats)
	v1.GET("/notification-channels", s.listNotificationChannels)
	v1.POST("/notification-channels", s.createNotificationChannel)
	v1.PUT("/notification-channels/:id", s.updateNotificationChannel)
	v1.DELETE("/notification-channels/:id", s.deleteNotificationChannel)
	v1.POST("/notification-channels/:id/test", s.testNotificationChannel)
	v1.POST("/api-keys", s.createAPIKey)
	v1.GET("/api-keys", s.listAPIKeys)
	v1.DELETE("/api-keys/:id", s.revokeAPIKey)
	v1.GET("/workers/status", s.workersStatus)
	return r
}

// workersDashboard serves the embedded static HTML page. The asset is
// versioned with the binary and cached briefly so a fast reload doesn't
// re-fetch it from the server.
func (s *Server) workersDashboard(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=30")
	c.Data(http.StatusOK, "text/html; charset=utf-8", web.WorkersHTML)
}

func (s *Server) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	dbStatus := "ok"
	if err := s.store.Ping(ctx); err != nil {
		dbStatus = "error"
		s.logger.Warn("health: db ping failed", "request_id", c.GetString(requestIDKey), "error", err)
	}
	redisStatus := "ok"
	if s.redis != nil {
		if err := s.redis.Ping(ctx).Err(); err != nil {
			redisStatus = "error"
			s.logger.Warn("health: redis ping failed", "request_id", c.GetString(requestIDKey), "error", err)
		}
	}
	status := http.StatusOK
	if dbStatus != "ok" || redisStatus != "ok" {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{
		"status":   statusLabel(status),
		"version":  s.cfg.Version,
		"database": dbStatus,
		"redis":    redisStatus,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) pingEndpoint(c *gin.Context) {
	var req struct {
		Endpoint string `json:"endpoint" binding:"required,url"`
	}
	if !bind(c, &req) {
		return
	}
	result, err := s.monitor.RunCheck(c.Request.Context(), models.Monitor{
		Type:             models.MonitorHTTP,
		Target:           req.Endpoint,
		Method:           http.MethodGet,
		ExpectedStatus:   http.StatusOK,
		TimeoutSeconds:   s.cfg.DefaultCheckTimeoutSeconds,
		FailureThreshold: 1,
	})
	writeCheck(c, result, err)
}

func (s *Server) manualCheck(c *gin.Context) {
	var monitor models.Monitor
	if !bind(c, &monitor) {
		return
	}
	result, err := s.monitor.RunCheck(c.Request.Context(), monitor)
	writeCheck(c, result, err)
}

func (s *Server) listMonitors(c *gin.Context) {
	items, err := s.store.ListMonitors(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) createMonitor(c *gin.Context) {
	var monitor models.Monitor
	if !bind(c, &monitor) {
		return
	}
	// Status is server-managed; never trust the client-supplied value.
	monitor.Status = ""
	monitor.ID = ""
	created, err := s.store.CreateMonitor(c.Request.Context(), monitor)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) getMonitor(c *gin.Context) {
	item, err := s.store.GetMonitor(c.Request.Context(), c.Param("id"))
	s.respond(c, item, err)
}

func (s *Server) updateMonitor(c *gin.Context) {
	var monitor models.Monitor
	if !bind(c, &monitor) {
		return
	}
	monitor.ID = c.Param("id")
	updated, err := s.store.UpdateMonitor(c.Request.Context(), monitor)
	s.respond(c, updated, err)
}

func (s *Server) deleteMonitor(c *gin.Context) {
	if err := s.store.DeleteMonitor(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) checkNow(c *gin.Context) {
	monitor, err := s.store.GetMonitor(c.Request.Context(), c.Param("id"))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	result, err := s.monitor.RunCheck(c.Request.Context(), monitor)
	writeCheck(c, result, err)
}

func (s *Server) monitorResults(c *gin.Context) {
	filter := resultFilter(c)
	filter.MonitorID = c.Param("id")
	results, err := s.store.ListCheckResults(c.Request.Context(), filter)
	s.respond(c, results, err)
}

func (s *Server) checkResults(c *gin.Context) {
	results, err := s.store.ListCheckResults(c.Request.Context(), resultFilter(c))
	s.respond(c, results, err)
}

func (s *Server) listIncidents(c *gin.Context) {
	items, err := s.store.ListIncidents(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) getIncident(c *gin.Context) {
	item, err := s.store.GetIncident(c.Request.Context(), c.Param("id"))
	s.respond(c, item, err)
}

func (s *Server) resolveIncident(c *gin.Context) {
	item, err := s.store.ResolveIncident(c.Request.Context(), c.Param("id"))
	s.respond(c, item, err)
}

func (s *Server) overviewStats(c *gin.Context) {
	stats, err := s.store.OverviewStats(c.Request.Context())
	if err == nil && s.metrics != nil {
		s.metrics.OpenIncidents.Set(float64(stats.OpenIncidents))
	}
	s.respond(c, stats, err)
}

func (s *Server) monitorStats(c *gin.Context) {
	filter := models.ResultFilter{MonitorID: c.Param("id"), Limit: 500}
	results, err := s.store.ListCheckResults(c.Request.Context(), filter)
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	total, up, sum := len(results), 0, int64(0)
	for _, result := range results {
		if result.Success {
			up++
		}
		sum += result.ResponseTimeMS
	}
	avg := float64(0)
	if total > 0 {
		avg = float64(sum) / float64(total)
	}
	c.JSON(http.StatusOK, gin.H{
		"monitorId":         c.Param("id"),
		"checks":            total,
		"successfulChecks":  up,
		"uptimePercentage":  percentage(up, total),
		"averageResponseMs": avg,
	})
}

func (s *Server) listNotificationChannels(c *gin.Context) {
	items, err := s.store.ListNotificationChannels(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) createNotificationChannel(c *gin.Context) {
	var channel models.NotificationChannel
	if !bind(c, &channel) {
		return
	}
	channel.ID = ""
	created, err := s.store.CreateNotificationChannel(c.Request.Context(), channel)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) updateNotificationChannel(c *gin.Context) {
	var channel models.NotificationChannel
	if !bind(c, &channel) {
		return
	}
	channel.ID = c.Param("id")
	updated, err := s.store.UpdateNotificationChannel(c.Request.Context(), channel)
	s.respond(c, updated, err)
}

func (s *Server) deleteNotificationChannel(c *gin.Context) {
	if err := s.store.DeleteNotificationChannel(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) testNotificationChannel(c *gin.Context) {
	channel, err := s.store.GetNotificationChannel(c.Request.Context(), c.Param("id"))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "channel": channel.ID})
}

func (s *Server) createAPIKey(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required,min=1,max=64"`
	}
	if !bind(c, &req) {
		return
	}
	raw, err := auth.NewRawKey()
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	key, err := s.store.CreateAPIKey(c.Request.Context(), models.APIKey{Name: req.Name, KeyHash: auth.Hash(raw)})
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	// `key` is shown ONCE to the caller; never returned by list/get.
	c.JSON(http.StatusCreated, gin.H{"id": key.ID, "name": key.Name, "key": raw, "createdAt": key.CreatedAt})
}

func (s *Server) listAPIKeys(c *gin.Context) {
	keys, err := s.store.ListAPIKeys(c.Request.Context())
	s.respond(c, keys, err)
}

func (s *Server) revokeAPIKey(c *gin.Context) {
	if err := s.store.RevokeAPIKey(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// apiKeyAuth authenticates every /api/v1 request. The bootstrap key is
// compared with constant-time equality; database keys are matched by SHA-256
// hash, also via constant-time comparison inside auth.Matches.
func (s *Server) apiKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearer(c.GetHeader("Authorization"))
		if raw == "" {
			raw = strings.TrimSpace(c.GetHeader("X-API-Key"))
		}
		if raw == "" {
			s.unauthorized(c, "api key required")
			return
		}
		if len(raw) > 256 {
			// Avoid hashing pathologically large headers.
			s.unauthorized(c, "invalid api key")
			return
		}
		if subtle.ConstantTimeCompare([]byte(raw), []byte(s.cfg.BootstrapAPIKey)) == 1 {
			c.Next()
			return
		}
		key, err := s.store.FindAPIKeyByHash(c.Request.Context(), auth.Hash(raw))
		if err != nil {
			s.logger.Error("auth: lookup failed", "request_id", c.GetString(requestIDKey), "error", err)
			s.unauthorized(c, "invalid api key")
			return
		}
		if key == nil {
			s.unauthorized(c, "invalid api key")
			return
		}
		// Fire-and-forget; touch failures should not block authenticated
		// requests but we still want them in logs.
		if err := s.store.TouchAPIKey(c.Request.Context(), key.ID); err != nil {
			s.logger.Warn("auth: touch api key failed", "request_id", c.GetString(requestIDKey), "key_id", key.ID, "error", err)
		}
		c.Set("apiKeyID", key.ID)
		c.Next()
	}
}

func (s *Server) unauthorized(c *gin.Context, message string) {
	c.Header("WWW-Authenticate", `Bearer realm="uptime"`)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": message, "requestId": c.GetString(requestIDKey)})
}

func (s *Server) notFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "route not found", "requestId": c.GetString(requestIDKey)})
}

func (s *Server) methodNotAllowed(c *gin.Context) {
	c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed", "requestId": c.GetString(requestIDKey)})
}

func (s *Server) requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		// Reject unreasonably long client-supplied request ids to keep logs
		// tidy and prevent abuse.
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
		}
		c.Header("X-Request-ID", requestID)
		c.Set(requestIDKey, requestID)
		c.Next()
	}
}

// bodyLimit caps the request body to cfg.MaxRequestBodyBytes. The limit is
// enforced lazily by http.MaxBytesReader, which makes ReadAll fail with a
// clear *http.MaxBytesError once exceeded.
func (s *Server) bodyLimit() gin.HandlerFunc {
	limit := s.cfg.MaxRequestBodyBytes
	return func(c *gin.Context) {
		if c.Request.Body != nil && limit > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

// recovery converts panics into 500 responses and structured logs. Replacing
// gin.Recovery() lets us include the request id in the panic log line.
func (s *Server) recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("panic recovered",
					"request_id", c.GetString(requestIDKey),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"panic", r,
				)
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
						"error":     "internal server error",
						"requestId": c.GetString(requestIDKey),
					})
				}
			}
		}()
		c.Next()
	}
}

func (s *Server) logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		s.logger.Info("request",
			"request_id", c.GetString(requestIDKey),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		)
	}
}

func bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		// Distinguish body-size violations from validation errors so
		// callers see the right status code.
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":     "request body exceeds limit",
				"limit":     maxBytesErr.Limit,
				"requestId": c.GetString(requestIDKey),
			})
			return false
		}
		if errors.Is(err, io.EOF) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":     "request body is required",
				"requestId": c.GetString(requestIDKey),
			})
			return false
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error":     "invalid request body: " + err.Error(),
			"requestId": c.GetString(requestIDKey),
		})
		return false
	}
	return true
}

func (s *Server) respond(c *gin.Context, payload any, err error) {
	s.respondStatus(c, http.StatusOK, payload, err)
}

func (s *Server) respondStatus(c *gin.Context, status int, payload any, err error) {
	if err != nil {
		code := apierr.StatusFor(err)
		message := apierr.PublicMessage(err)
		if code >= 500 {
			s.logger.Error("handler error",
				"request_id", c.GetString(requestIDKey),
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"error", err,
			)
		}
		c.JSON(code, gin.H{"error": message, "requestId": c.GetString(requestIDKey)})
		return
	}
	c.JSON(status, payload)
}

// writeCheck handles the legacy ping-style endpoints whose response is the
// CheckResult itself. A failure during validation (e.g. blocked target) is a
// 400; a non-2xx result with a captured CheckResult is still a 200 because
// the client asked us to perform the check and we did.
func writeCheck(c *gin.Context, result models.CheckResult, err error) {
	if err != nil {
		if result.MonitorID == "" && result.Status == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "requestId": c.GetString(requestIDKey)})
			return
		}
		if result.Error == "" {
			result.Error = err.Error()
		}
	}
	c.JSON(http.StatusOK, result)
}

func bearer(header string) string {
	if len(header) >= 7 && strings.EqualFold(header[:7], "Bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

func resultFilter(c *gin.Context) models.ResultFilter {
	filter := models.ResultFilter{
		MonitorID: c.Query("monitorId"),
		Status:    c.Query("status"),
		Limit:     atoiDefault(c.Query("limit"), 100),
		Offset:    atoiDefault(c.Query("offset"), 0),
	}
	if filter.Limit < 1 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if value := c.Query("checkedAfter"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			filter.CheckedAfter = &parsed
		}
	}
	if value := c.Query("checkedBefore"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			filter.CheckedBefore = &parsed
		}
	}
	return filter
}

func atoiDefault(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func percentage(success, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(success) / float64(total) * 100
}

func statusLabel(code int) string {
	if code >= 200 && code < 300 {
		return "ok"
	}
	return "degraded"
}
