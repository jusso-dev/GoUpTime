package api

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/models"
)

func (s *Server) listServices(c *gin.Context) {
	items, err := s.store.ListServices(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) getService(c *gin.Context) {
	item, err := s.store.GetService(c.Request.Context(), c.Param("id"))
	s.respond(c, item, err)
}

func (s *Server) createService(c *gin.Context) {
	var item models.Service
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	created, err := s.store.CreateService(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) updateService(c *gin.Context) {
	var item models.Service
	if !bind(c, &item) {
		return
	}
	item.ID = c.Param("id")
	updated, err := s.store.UpdateService(c.Request.Context(), item)
	s.respond(c, updated, err)
}

func (s *Server) deleteService(c *gin.Context) {
	if err := s.store.DeleteService(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) getMaintenanceWindow(c *gin.Context) {
	item, err := s.store.GetMaintenanceWindow(c.Request.Context(), c.Param("id"))
	s.respond(c, item, err)
}

func (s *Server) updateMaintenanceWindow(c *gin.Context) {
	var item models.MaintenanceWindow
	if !bind(c, &item) {
		return
	}
	item.ID = c.Param("id")
	updated, err := s.store.UpdateMaintenanceWindow(c.Request.Context(), item)
	s.respond(c, updated, err)
}

func (s *Server) getStatusPage(c *gin.Context) {
	item, err := s.store.GetStatusPage(c.Request.Context(), c.Param("id"))
	s.respond(c, item, err)
}

func (s *Server) updateStatusPage(c *gin.Context) {
	var item models.StatusPage
	if !bind(c, &item) {
		return
	}
	item.ID = c.Param("id")
	updated, err := s.store.UpdateStatusPage(c.Request.Context(), item)
	s.respond(c, updated, err)
}

func (s *Server) createStatusPageComponent(c *gin.Context) {
	var item models.StatusPageComponent
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	item.StatusPageID = c.Param("id")
	created, err := s.store.CreateStatusPageComponent(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) updateStatusPageComponent(c *gin.Context) {
	var item models.StatusPageComponent
	if !bind(c, &item) {
		return
	}
	item.ID = c.Param("componentId")
	item.StatusPageID = c.Param("id")
	updated, err := s.store.UpdateStatusPageComponent(c.Request.Context(), item)
	s.respond(c, updated, err)
}

func (s *Server) uptimeReport(c *gin.Context) {
	filter, ok := reportFilter(c)
	if !ok {
		return
	}
	report, err := s.store.UptimeReport(c.Request.Context(), filter)
	s.respond(c, report, err)
}

func (s *Server) exportCheckResults(c *gin.Context) {
	format := c.DefaultQuery("format", "json")
	results, err := s.store.ExportCheckResults(c.Request.Context(), resultFilter(c))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	if format == "csv" {
		c.Data(http.StatusOK, "text/csv; charset=utf-8", checkResultsCSV(results))
		return
	}
	c.JSON(http.StatusOK, results)
}

func (s *Server) exportIncidents(c *gin.Context) {
	format := c.DefaultQuery("format", "json")
	incidents, err := s.store.ExportIncidents(c.Request.Context(), resultFilter(c))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	if format == "csv" {
		c.Data(http.StatusOK, "text/csv; charset=utf-8", incidentsCSV(incidents))
		return
	}
	c.JSON(http.StatusOK, incidents)
}

func reportFilter(c *gin.Context) (models.UptimeReportFilter, bool) {
	now := time.Now().UTC()
	filter := models.UptimeReportFilter{
		MonitorID:          c.Query("monitorId"),
		ServiceID:          c.Query("serviceId"),
		StatusPageID:       c.Query("statusPageId"),
		From:               now.Add(-24 * time.Hour),
		To:                 now,
		ExcludeMaintenance: c.Query("excludeMaintenance") == "true",
	}
	if value := c.Query("from"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "from must be RFC3339", "requestId": c.GetString(requestIDKey)})
			return filter, false
		}
		filter.From = parsed
	}
	if value := c.Query("to"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to must be RFC3339", "requestId": c.GetString(requestIDKey)})
			return filter, false
		}
		filter.To = parsed
	}
	return filter, true
}

func checkResultsCSV(results []models.CheckResult) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"id", "monitor_id", "status", "success", "response_time_ms", "status_code", "error", "checked_at", "maintenance_suppressed"})
	for _, r := range results {
		_ = w.Write([]string{r.ID, r.MonitorID, string(r.Status), strconv.FormatBool(r.Success), strconv.FormatInt(r.ResponseTimeMS, 10), strconv.Itoa(r.StatusCode), r.Error, r.CheckedAt.Format(time.RFC3339), strconv.FormatBool(r.MaintenanceSuppressed)})
	}
	w.Flush()
	return b.Bytes()
}

func incidentsCSV(incidents []models.Incident) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"id", "monitor_id", "status", "started_at", "resolved_at", "reason", "last_error", "consecutive_failures"})
	for _, i := range incidents {
		resolved := ""
		if i.ResolvedAt != nil {
			resolved = i.ResolvedAt.Format(time.RFC3339)
		}
		_ = w.Write([]string{i.ID, i.MonitorID, string(i.Status), i.StartedAt.Format(time.RFC3339), resolved, i.Reason, i.LastError, strconv.Itoa(i.ConsecutiveFailures)})
	}
	w.Flush()
	return b.Bytes()
}
