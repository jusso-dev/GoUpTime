// Heartbeat (push monitor) HTTP endpoints.
//
// Three surfaces:
//   1. POST/GET /api/v1/heartbeats/:token/ping — public, no auth, the
//      cron/CI client hits this at the expected cadence.
//   2. POST /api/v1/monitors/:id/heartbeat — admin, regenerates the token
//      and returns it once (subsequent reads return only metadata).
//   3. GET  /api/v1/monitors/:id/heartbeat — returns interval/grace/
//      last_ping_at without the secret.

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

const heartbeatTokenPrefix = "hb_"

// heartbeatPingRateLimitTTL caps how often a single token can ping us;
// anything faster is almost certainly a misconfigured loop. Enforced via
// Redis SET NX EX when the client is available.
const heartbeatPingRateLimitTTL = 1 * time.Second

func (s *Server) heartbeatPing(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if token == "" || !strings.HasPrefix(token, heartbeatTokenPrefix) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "heartbeat not found"})
		return
	}
	hash := hashToken(token)

	// Per-token rate limit to defend against runaway scripts.
	if s.redis != nil {
		key := "rl:hb:" + hash
		ok, err := s.redis.SetNX(c.Request.Context(), key, "1", heartbeatPingRateLimitTTL).Result()
		if err == nil && !ok {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit: minimum 1s between pings"})
			return
		}
	}

	monitorID, err := s.store.RecordHeartbeatPing(c.Request.Context(),
		hash, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "heartbeat not found"})
			return
		}
		s.respond(c, nil, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "monitorId": monitorID, "receivedAt": time.Now().UTC()})
}

func (s *Server) getMonitorHeartbeat(c *gin.Context) {
	// Verify the monitor belongs to the caller's org first — store layer
	// already enforces it, but reading the monitor here gives us a 404
	// instead of an oddly-shaped error if the monitor doesn't exist.
	if _, err := s.store.GetMonitor(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	hb, err := s.store.GetHeartbeat(auth.WithSystem(c.Request.Context()), c.Param("id"))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	c.JSON(http.StatusOK, hb)
}

func (s *Server) setMonitorHeartbeat(c *gin.Context) {
	var req struct {
		ExpectedIntervalSeconds int  `json:"expectedIntervalSeconds" binding:"required,min=10,max=86400"`
		GraceSeconds            int  `json:"graceSeconds" binding:"omitempty,min=0,max=3600"`
		Regenerate              bool `json:"regenerate"`
	}
	if !bind(c, &req) {
		return
	}
	// Resolve monitor (also enforces org).
	monitor, err := s.store.GetMonitor(c.Request.Context(), c.Param("id"))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	if monitor.Type != models.MonitorHeartbeat {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "monitor is not of type heartbeat"})
		return
	}

	sysCtx := auth.WithSystem(c.Request.Context())
	rawToken := ""
	existing, getErr := s.store.GetHeartbeat(sysCtx, monitor.ID)
	if getErr != nil && !errors.Is(getErr, apierr.ErrNotFound) {
		s.respond(c, nil, getErr)
		return
	}
	// Issue a new token if there is none, or if the client asked to rotate.
	if errors.Is(getErr, apierr.ErrNotFound) || req.Regenerate {
		raw, err := auth.NewRawKey()
		if err != nil {
			s.respond(c, nil, err)
			return
		}
		// Re-prefix with hb_ so the public endpoint can quickly reject
		// random garbage before hashing.
		rawToken = heartbeatTokenPrefix + strings.TrimPrefix(raw, "upt_")
		existing.TokenHash = hashToken(rawToken)
	}
	existing.MonitorID = monitor.ID
	existing.ExpectedIntervalSeconds = req.ExpectedIntervalSeconds
	existing.GraceSeconds = req.GraceSeconds
	saved, err := s.store.SetHeartbeat(sysCtx, existing)
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	resp := gin.H{
		"monitorId":               saved.MonitorID,
		"expectedIntervalSeconds": saved.ExpectedIntervalSeconds,
		"graceSeconds":            saved.GraceSeconds,
		"lastPingAt":              saved.LastPingAt,
	}
	if rawToken != "" {
		// Returned exactly once; subsequent GETs only get metadata.
		resp["token"] = rawToken
		resp["pingUrl"] = strings.TrimRight(s.cfg.AppBaseURL, "/") + "/api/v1/heartbeats/" + rawToken + "/ping"
	}
	c.JSON(http.StatusOK, resp)
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
