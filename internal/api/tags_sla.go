// Tag CRUD + SLA reporting HTTP handlers. Tag filtering on /monitors
// also lives here because the implementation is essentially the same
// query — it just defers to a different repository entry point.

package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
	"github.com/jusso-dev/uptime/internal/repository"
)

func (s *Server) listTags(c *gin.Context) {
	tags, err := s.store.ListTags(c.Request.Context())
	s.respond(c, tags, err)
}

func (s *Server) createTag(c *gin.Context) {
	var req struct {
		Name  string `json:"name" binding:"required,min=1,max=64"`
		Color string `json:"color" binding:"omitempty,len=7,startswith=#"`
	}
	if !bind(c, &req) {
		return
	}
	created, err := s.store.CreateTag(c.Request.Context(), models.Tag{
		Name:  req.Name,
		Color: req.Color,
	})
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) deleteTag(c *gin.Context) {
	if err := s.store.DeleteTag(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) setMonitorTags(c *gin.Context) {
	var req struct {
		TagIDs []string `json:"tagIds"`
	}
	if !bind(c, &req) {
		return
	}
	if err := s.store.SetMonitorTags(c.Request.Context(), c.Param("id"), req.TagIDs); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Replaces the existing listMonitors when a ?tag= filter is present.
// The router wires this transparently: if no tag query is supplied, the
// original listMonitors is called.
func (s *Server) listMonitorsFiltered(c *gin.Context) {
	if tagCSV := c.Query("tag"); tagCSV != "" {
		names := repository.SplitCSV(tagCSV)
		items, err := s.store.ListMonitorsByTags(c.Request.Context(), names)
		s.respond(c, items, err)
		return
	}
	s.listMonitors(c)
}

func (s *Server) slaForMonitor(c *gin.Context) {
	from, to, period, err := slaWindow(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error(), "requestId": c.GetString(requestIDKey)})
		return
	}
	report, err := s.store.SLAReportForMonitor(c.Request.Context(), c.Param("id"), from, to)
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	report.Period = period
	c.JSON(http.StatusOK, report)
}

func (s *Server) slaForOrg(c *gin.Context) {
	from, to, period, err := slaWindow(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error(), "requestId": c.GetString(requestIDKey)})
		return
	}
	report, err := s.store.SLAReportForOrg(c.Request.Context(), from, to)
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	report.Period = period
	c.JSON(http.StatusOK, report)
}

// slaWindow parses ?period=30d|month|quarter|year|custom plus optional
// ?from= and ?to= for custom windows. Returns the [from, to] range
// along with a stable period label for the JSON response.
func slaWindow(c *gin.Context) (time.Time, time.Time, string, error) {
	period := strings.ToLower(c.DefaultQuery("period", "30d"))
	now := time.Now().UTC()
	switch period {
	case "24h":
		return now.Add(-24 * time.Hour), now, period, nil
	case "7d":
		return now.Add(-7 * 24 * time.Hour), now, period, nil
	case "30d", "":
		return now.Add(-30 * 24 * time.Hour), now, "30d", nil
	case "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, now, period, nil
	case "quarter":
		q := ((int(now.Month()) - 1) / 3) * 3
		start := time.Date(now.Year(), time.Month(q+1), 1, 0, 0, 0, 0, time.UTC)
		return start, now, period, nil
	case "year":
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return start, now, period, nil
	case "custom":
		from, err := time.Parse(time.RFC3339, c.Query("from"))
		if err != nil {
			return time.Time{}, time.Time{}, "", apierr.ErrInvalidInput
		}
		to, err := time.Parse(time.RFC3339, c.Query("to"))
		if err != nil {
			return time.Time{}, time.Time{}, "", apierr.ErrInvalidInput
		}
		return from, to, period, nil
	default:
		return time.Time{}, time.Time{}, "", apierr.ErrInvalidInput
	}
}
