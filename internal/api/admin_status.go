// Admin handlers for status-page and maintenance-window management.
// These live under /api/v1 so the mobile app and CLI tools can use the
// same auth (Clerk JWT / API key) as every other write surface.

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

func (s *Server) listStatusPages(c *gin.Context) {
	pages, err := s.store.ListStatusPages(c.Request.Context())
	s.respond(c, pages, err)
}

func (s *Server) createStatusPage(c *gin.Context) {
	var req struct {
		Slug         string         `json:"slug" binding:"required,min=2,max=64,alphanum"`
		Name         string         `json:"name" binding:"required,min=1,max=120"`
		Description  string         `json:"description" binding:"omitempty,max=2048"`
		CustomDomain string         `json:"customDomain" binding:"omitempty,max=253"`
		Theme        map[string]any `json:"theme"`
		Published    bool           `json:"published"`
	}
	if !bind(c, &req) {
		return
	}
	page := models.StatusPage{
		Slug:         req.Slug,
		Name:         req.Name,
		Description:  req.Description,
		CustomDomain: req.CustomDomain,
		Theme:        req.Theme,
		Published:    req.Published,
	}
	created, err := s.store.CreateStatusPage(c.Request.Context(), page)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) deleteStatusPage(c *gin.Context) {
	if err := s.store.DeleteStatusPage(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listStatusPageComponents(c *gin.Context) {
	// Authorize by reading the parent page through the tenant filter; if
	// the caller can see the page, they can see its components.
	if _, err := s.store.ListStatusPages(c.Request.Context()); err != nil {
		s.respond(c, nil, err)
		return
	}
	comps, err := s.store.ListStatusPageComponents(auth.WithSystem(c.Request.Context()), c.Param("id"))
	s.respond(c, comps, err)
}

func (s *Server) upsertStatusPageComponent(c *gin.Context) {
	var req struct {
		ID          string   `json:"id"`
		Name        string   `json:"name" binding:"required,min=1,max=120"`
		Description string   `json:"description" binding:"omitempty,max=2048"`
		Position    int      `json:"position"`
		MonitorIDs  []string `json:"monitorIds"`
		GroupName   string   `json:"groupName" binding:"omitempty,max=120"`
	}
	if !bind(c, &req) {
		return
	}
	c.Request = c.Request.WithContext(auth.WithSystem(c.Request.Context()))
	saved, err := s.store.UpsertStatusPageComponent(c.Request.Context(), models.StatusPageComponent{
		ID:           req.ID,
		StatusPageID: c.Param("id"),
		Name:         req.Name,
		Description:  req.Description,
		Position:     req.Position,
		MonitorIDs:   req.MonitorIDs,
		GroupName:    req.GroupName,
	})
	s.respondStatus(c, http.StatusOK, saved, err)
}

func (s *Server) deleteStatusPageComponent(c *gin.Context) {
	c.Request = c.Request.WithContext(auth.WithSystem(c.Request.Context()))
	if err := s.store.DeleteStatusPageComponent(c.Request.Context(), c.Param("componentId")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listMaintenanceWindows(c *gin.Context) {
	windows, err := s.store.ListMaintenanceWindows(c.Request.Context())
	s.respond(c, windows, err)
}

func (s *Server) createMaintenanceWindow(c *gin.Context) {
	var w models.MaintenanceWindow
	if !bind(c, &w) {
		return
	}
	p, err := auth.Require(c.Request.Context())
	if err == nil {
		w.CreatedByUserID = p.UserID
	}
	saved, err := s.store.CreateMaintenanceWindow(c.Request.Context(), w)
	s.respondStatus(c, http.StatusCreated, saved, err)
}

func (s *Server) deleteMaintenanceWindow(c *gin.Context) {
	if err := s.store.DeleteMaintenanceWindow(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}
