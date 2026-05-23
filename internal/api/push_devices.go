// Mobile push device registration. The Expo client SDK in the mobile
// app calls these endpoints to (a) register its push token on sign-in
// and (b) clean it up on sign-out / uninstall.

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

func (s *Server) listPushDevices(c *gin.Context) {
	p, err := auth.Require(c.Request.Context())
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	if p.UserID == "" {
		c.JSON(http.StatusOK, []models.PushDevice{})
		return
	}
	devices, err := s.store.ListPushDevicesForUser(c.Request.Context(), p.UserID)
	s.respond(c, devices, err)
}

func (s *Server) registerPushDevice(c *gin.Context) {
	var req struct {
		Platform   string `json:"platform" binding:"required,oneof=ios android"`
		Token      string `json:"token" binding:"required,min=4,max=512"`
		AppVersion string `json:"appVersion" binding:"omitempty,max=64"`
	}
	if !bind(c, &req) {
		return
	}
	p, err := auth.Require(c.Request.Context())
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	if p.UserID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "push devices require a user-scoped session"})
		return
	}
	device, err := s.store.UpsertPushDevice(c.Request.Context(), models.PushDevice{
		UserID:     p.UserID,
		Platform:   req.Platform,
		ExpoToken:  req.Token,
		AppVersion: req.AppVersion,
	})
	s.respondStatus(c, http.StatusCreated, device, err)
}

func (s *Server) deletePushDevice(c *gin.Context) {
	if err := s.store.DeletePushDevice(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}
