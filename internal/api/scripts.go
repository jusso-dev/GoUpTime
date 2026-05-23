// HTTP handlers for the per-monitor script tables: multistep DSL and
// browser (Playwright) source code. Both follow the same shape: GET
// returns the stored object, PUT replaces it. The monitor itself is
// fetched first so cross-org access returns the same 404 the rest of
// the API does.

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

func (s *Server) getMonitorMultistep(c *gin.Context) {
	if _, err := s.store.GetMonitor(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	script, err := s.store.GetMultistepScript(auth.WithSystem(c.Request.Context()), c.Param("id"))
	s.respond(c, script, err)
}

func (s *Server) setMonitorMultistep(c *gin.Context) {
	var steps models.MultistepSteps
	if !bind(c, &steps) {
		return
	}
	if _, err := s.store.GetMonitor(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	saved, err := s.store.SetMultistepScript(auth.WithSystem(c.Request.Context()), models.MultistepScript{
		MonitorID: c.Param("id"),
		Steps:     steps,
	})
	s.respondStatus(c, http.StatusOK, saved, err)
}

func (s *Server) getMonitorBrowserScript(c *gin.Context) {
	if _, err := s.store.GetMonitor(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	script, err := s.store.GetBrowserScript(auth.WithSystem(c.Request.Context()), c.Param("id"))
	s.respond(c, script, err)
}

func (s *Server) setMonitorBrowserScript(c *gin.Context) {
	var req struct {
		Source string `json:"source" binding:"required,max=65536"`
	}
	if !bind(c, &req) {
		return
	}
	if _, err := s.store.GetMonitor(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	saved, err := s.store.SetBrowserScript(auth.WithSystem(c.Request.Context()), models.BrowserScript{
		MonitorID: c.Param("id"),
		Source:    req.Source,
	})
	s.respondStatus(c, http.StatusOK, saved, err)
}
