package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/config"
	"github.com/jusso-dev/uptime/internal/metrics"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
	"github.com/jusso-dev/uptime/internal/service"
)

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
	r.Use(s.requestID(), gin.Recovery(), s.logging(), m.GinMiddleware())
	r.GET("/health", s.health)
	r.GET("/health-check", s.health)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.POST("/ping-endpoint", s.pingEndpoint)

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
	return r
}

func (s *Server) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	dbStatus := "ok"
	if err := s.store.Ping(ctx); err != nil {
		dbStatus = "error"
	}
	redisStatus := "ok"
	if s.redis != nil {
		if err := s.redis.Ping(ctx).Err(); err != nil {
			redisStatus = "error"
		}
	}
	status := http.StatusOK
	if dbStatus != "ok" || redisStatus != "ok" {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{
		"status":   "ok",
		"version":  s.cfg.Version,
		"database": dbStatus,
		"redis":    redisStatus,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) pingEndpoint(c *gin.Context) {
	var req struct {
		Endpoint string `json:"endpoint" binding:"required"`
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
	respond(c, items, err)
}

func (s *Server) createMonitor(c *gin.Context) {
	var monitor models.Monitor
	if !bind(c, &monitor) {
		return
	}
	created, err := s.store.CreateMonitor(c.Request.Context(), monitor)
	respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) getMonitor(c *gin.Context) {
	item, err := s.store.GetMonitor(c.Request.Context(), c.Param("id"))
	respond(c, item, err)
}

func (s *Server) updateMonitor(c *gin.Context) {
	var monitor models.Monitor
	if !bind(c, &monitor) {
		return
	}
	monitor.ID = c.Param("id")
	updated, err := s.store.UpdateMonitor(c.Request.Context(), monitor)
	respond(c, updated, err)
}

func (s *Server) deleteMonitor(c *gin.Context) {
	respond(c, gin.H{"deleted": true}, s.store.DeleteMonitor(c.Request.Context(), c.Param("id")))
}

func (s *Server) checkNow(c *gin.Context) {
	monitor, err := s.store.GetMonitor(c.Request.Context(), c.Param("id"))
	if err != nil {
		respond(c, nil, err)
		return
	}
	result, err := s.monitor.RunCheck(c.Request.Context(), monitor)
	writeCheck(c, result, err)
}

func (s *Server) monitorResults(c *gin.Context) {
	filter := resultFilter(c)
	filter.MonitorID = c.Param("id")
	results, err := s.store.ListCheckResults(c.Request.Context(), filter)
	respond(c, results, err)
}

func (s *Server) checkResults(c *gin.Context) {
	results, err := s.store.ListCheckResults(c.Request.Context(), resultFilter(c))
	respond(c, results, err)
}

func (s *Server) listIncidents(c *gin.Context) {
	items, err := s.store.ListIncidents(c.Request.Context())
	respond(c, items, err)
}

func (s *Server) getIncident(c *gin.Context) {
	item, err := s.store.GetIncident(c.Request.Context(), c.Param("id"))
	respond(c, item, err)
}

func (s *Server) resolveIncident(c *gin.Context) {
	item, err := s.store.ResolveIncident(c.Request.Context(), c.Param("id"))
	respond(c, item, err)
}

func (s *Server) overviewStats(c *gin.Context) {
	stats, err := s.store.OverviewStats(c.Request.Context())
	if err == nil && s.metrics != nil {
		s.metrics.OpenIncidents.Set(float64(stats.OpenIncidents))
	}
	respond(c, stats, err)
}

func (s *Server) monitorStats(c *gin.Context) {
	filter := models.ResultFilter{MonitorID: c.Param("id"), Limit: 500}
	results, err := s.store.ListCheckResults(c.Request.Context(), filter)
	if err != nil {
		respond(c, nil, err)
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
	c.JSON(http.StatusOK, gin.H{"monitorId": c.Param("id"), "checks": total, "successfulChecks": up, "uptimePercentage": percentage(up, total), "averageResponseMs": avg})
}

func (s *Server) listNotificationChannels(c *gin.Context) {
	items, err := s.store.ListNotificationChannels(c.Request.Context())
	respond(c, items, err)
}

func (s *Server) createNotificationChannel(c *gin.Context) {
	var channel models.NotificationChannel
	if !bind(c, &channel) {
		return
	}
	created, err := s.store.CreateNotificationChannel(c.Request.Context(), channel)
	respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) updateNotificationChannel(c *gin.Context) {
	var channel models.NotificationChannel
	if !bind(c, &channel) {
		return
	}
	channel.ID = c.Param("id")
	updated, err := s.store.UpdateNotificationChannel(c.Request.Context(), channel)
	respond(c, updated, err)
}

func (s *Server) deleteNotificationChannel(c *gin.Context) {
	respond(c, gin.H{"deleted": true}, s.store.DeleteNotificationChannel(c.Request.Context(), c.Param("id")))
}

func (s *Server) testNotificationChannel(c *gin.Context) {
	channel, err := s.findNotificationChannel(c.Request.Context(), c.Param("id"))
	if err != nil {
		respond(c, nil, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "channel": channel.ID})
}

func (s *Server) createAPIKey(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if !bind(c, &req) {
		return
	}
	raw, err := auth.NewRawKey()
	if err != nil {
		respond(c, nil, err)
		return
	}
	key, err := s.store.CreateAPIKey(c.Request.Context(), models.APIKey{Name: req.Name, KeyHash: auth.Hash(raw)})
	if err != nil {
		respond(c, nil, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": key.ID, "name": key.Name, "key": raw, "createdAt": key.CreatedAt})
}

func (s *Server) listAPIKeys(c *gin.Context) {
	keys, err := s.store.ListAPIKeys(c.Request.Context())
	respond(c, keys, err)
}

func (s *Server) revokeAPIKey(c *gin.Context) {
	respond(c, gin.H{"revoked": true}, s.store.RevokeAPIKey(c.Request.Context(), c.Param("id")))
}

func (s *Server) apiKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearer(c.GetHeader("Authorization"))
		if raw == "" {
			raw = c.GetHeader("X-API-Key")
		}
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api key required"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(raw), []byte(s.cfg.BootstrapAPIKey)) == 1 {
			c.Next()
			return
		}
		key, err := s.store.FindAPIKeyByHash(c.Request.Context(), auth.Hash(raw))
		if err != nil || key == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			return
		}
		_ = s.store.TouchAPIKey(c.Request.Context(), key.ID)
		c.Next()
	}
}

func (s *Server) requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Header("X-Request-ID", requestID)
		c.Set("requestID", requestID)
		c.Next()
	}
}

func (s *Server) logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		s.logger.Info("request",
			"request_id", c.GetString("requestID"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func respond(c *gin.Context, payload any, err error) {
	respondStatus(c, http.StatusOK, payload, err)
}

func respondStatus(c *gin.Context, status int, payload any, err error) {
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, context.Canceled) {
			code = http.StatusRequestTimeout
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, payload)
}

func writeCheck(c *gin.Context, result models.CheckResult, err error) {
	if err != nil && result.Error == "" {
		result.Error = err.Error()
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusBadRequest
	}
	c.JSON(status, result)
}

func bearer(header string) string {
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
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

func (s *Server) findNotificationChannel(ctx context.Context, id string) (models.NotificationChannel, error) {
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		return models.NotificationChannel{}, err
	}
	for _, channel := range channels {
		if channel.ID == id {
			return channel, nil
		}
	}
	return models.NotificationChannel{}, errors.New("notification channel not found")
}
