package api

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

type phase23Store interface {
	UpdateIncidentState(context.Context, string, models.IncidentTransition, string) (models.Incident, error)
	ListIncidentTimeline(context.Context, string, int, int) ([]models.IncidentTimelineEvent, error)
	ListIncidentComments(context.Context, string) ([]models.IncidentComment, error)
	CreateIncidentComment(context.Context, models.IncidentComment) (models.IncidentComment, error)
	UpdateIncidentComment(context.Context, models.IncidentComment) (models.IncidentComment, error)
	DeleteIncidentComment(context.Context, string, string) error
	GetIncidentPostmortem(context.Context, string) (models.IncidentPostmortem, error)
	UpsertIncidentPostmortem(context.Context, models.IncidentPostmortem) (models.IncidentPostmortem, error)
	ExportIncidentPostmortemMarkdown(context.Context, string) (string, error)
	ListIncidentActionItems(context.Context, string) ([]models.IncidentActionItem, error)
	CreateIncidentActionItem(context.Context, models.IncidentActionItem) (models.IncidentActionItem, error)
	UpdateIncidentActionItem(context.Context, models.IncidentActionItem) (models.IncidentActionItem, error)
	SetMonitorDependencies(context.Context, string, []string) ([]models.MonitorDependency, error)
	ListMonitorDependencies(context.Context, string) ([]models.MonitorDependency, error)
	CreateAgent(context.Context, models.Agent) (models.Agent, error)
	ListAgents(context.Context) ([]models.Agent, error)
	FindAgentByTokenHash(context.Context, string) (*models.Agent, error)
	TouchAgent(context.Context, string) error
	RevokeAgent(context.Context, string) error
	AgentJobs(context.Context, models.Agent, int) ([]models.AgentJob, error)
	CreateStatusPageSubscriber(context.Context, models.StatusPageSubscriber, string, string) (models.StatusPageSubscriber, error)
	ConfirmStatusPageSubscriber(context.Context, string) (models.StatusPageSubscriber, error)
	UnsubscribeStatusPageSubscriber(context.Context, string) error
	ListStatusPageSubscribers(context.Context, string) ([]models.StatusPageSubscriber, error)
	CreateStatusPageAnnouncement(context.Context, models.StatusPageAnnouncement) (models.StatusPageAnnouncement, error)
	ListStatusPageAnnouncements(context.Context, string, bool) ([]models.StatusPageAnnouncement, error)
	CreateOnCallSchedule(context.Context, models.OnCallSchedule) (models.OnCallSchedule, error)
	ListOnCallSchedules(context.Context) ([]models.OnCallSchedule, error)
	CreateOnCallOverride(context.Context, models.OnCallOverride) (models.OnCallOverride, error)
	ResolveOnCall(context.Context, string, time.Time) (models.OnCallShift, error)
	UpcomingOnCallShifts(context.Context, string, time.Time, int) ([]models.OnCallShift, error)
	CreateEscalationPolicy(context.Context, models.EscalationPolicy) (models.EscalationPolicy, error)
	ListEscalationPolicies(context.Context) ([]models.EscalationPolicy, error)
	CreateRunbook(context.Context, models.Runbook) (models.Runbook, error)
	ListRunbooks(context.Context) ([]models.Runbook, error)
	RunbooksForIncident(context.Context, string) ([]models.Runbook, error)
	CreateBrowserArtifact(context.Context, models.BrowserArtifact) (models.BrowserArtifact, error)
	GetBrowserArtifact(context.Context, string) (models.BrowserArtifact, error)
	DeleteExpiredBrowserArtifacts(context.Context, time.Time) (int64, error)
}

func (s *Server) phase23(c *gin.Context) (phase23Store, bool) {
	store, ok := s.store.(phase23Store)
	if !ok {
		s.respond(c, nil, apierr.ErrInvalidInput)
		return nil, false
	}
	return store, true
}

func (s *Server) transitionIncident(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var req models.IncidentTransition
	if !bind(c, &req) {
		return
	}
	p, _ := auth.Require(c.Request.Context())
	item, err := store.UpdateIncidentState(c.Request.Context(), c.Param("id"), req, p.UserID)
	s.respond(c, item, err)
}

func (s *Server) incidentTimeline(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListIncidentTimeline(c.Request.Context(), c.Param("id"), atoiDefault(c.Query("limit"), 100), atoiDefault(c.Query("offset"), 0))
	s.respond(c, items, err)
}

func (s *Server) listIncidentComments(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListIncidentComments(c.Request.Context(), c.Param("id"))
	s.respond(c, items, err)
}

func (s *Server) createIncidentComment(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.IncidentComment
	if !bind(c, &item) {
		return
	}
	p, _ := auth.Require(c.Request.Context())
	item.ID = ""
	item.IncidentID = c.Param("id")
	item.AuthorUserID = p.UserID
	created, err := store.CreateIncidentComment(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) updateIncidentComment(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.IncidentComment
	if !bind(c, &item) {
		return
	}
	item.ID = c.Param("commentId")
	item.IncidentID = c.Param("id")
	updated, err := store.UpdateIncidentComment(c.Request.Context(), item)
	s.respond(c, updated, err)
}

func (s *Server) deleteIncidentComment(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	if err := store.DeleteIncidentComment(c.Request.Context(), c.Param("id"), c.Param("commentId")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) getIncidentPostmortem(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	item, err := store.GetIncidentPostmortem(c.Request.Context(), c.Param("id"))
	s.respond(c, item, err)
}

func (s *Server) upsertIncidentPostmortem(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.IncidentPostmortem
	if !bind(c, &item) {
		return
	}
	p, _ := auth.Require(c.Request.Context())
	item.IncidentID = c.Param("id")
	item.UpdatedByUserID = p.UserID
	if item.CreatedByUserID == "" {
		item.CreatedByUserID = p.UserID
	}
	updated, err := store.UpsertIncidentPostmortem(c.Request.Context(), item)
	s.respond(c, updated, err)
}

func (s *Server) exportIncidentPostmortem(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	md, err := store.ExportIncidentPostmortemMarkdown(c.Request.Context(), c.Param("id"))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(md))
}

func (s *Server) listIncidentActionItems(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListIncidentActionItems(c.Request.Context(), c.Param("id"))
	s.respond(c, items, err)
}

func (s *Server) createIncidentActionItem(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.IncidentActionItem
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	item.IncidentID = c.Param("id")
	created, err := store.CreateIncidentActionItem(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) updateIncidentActionItem(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.IncidentActionItem
	if !bind(c, &item) {
		return
	}
	item.ID = c.Param("actionItemId")
	item.IncidentID = c.Param("id")
	if c.Query("complete") == "true" && item.CompletedAt == nil {
		now := time.Now().UTC()
		item.CompletedAt = &now
		if p, err := auth.Require(c.Request.Context()); err == nil {
			item.CompletedByUserID = p.UserID
		}
	}
	updated, err := store.UpdateIncidentActionItem(c.Request.Context(), item)
	s.respond(c, updated, err)
}

func (s *Server) incidentRunbooks(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.RunbooksForIncident(c.Request.Context(), c.Param("id"))
	s.respond(c, items, err)
}

func (s *Server) listRunbooks(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListRunbooks(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) createRunbook(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.Runbook
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	created, err := store.CreateRunbook(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) listMonitorDependencies(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListMonitorDependencies(c.Request.Context(), c.Param("id"))
	s.respond(c, items, err)
}

func (s *Server) setMonitorDependencies(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var req struct {
		DependsOnMonitorIDs []string `json:"dependsOnMonitorIds"`
	}
	if !bind(c, &req) {
		return
	}
	items, err := store.SetMonitorDependencies(c.Request.Context(), c.Param("id"), req.DependsOnMonitorIDs)
	s.respond(c, items, err)
}

func (s *Server) listAgents(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListAgents(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) createAgent(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.Agent
	if !bind(c, &item) {
		return
	}
	raw, err := auth.NewRawKey()
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	item.TokenHash = auth.Hash(raw)
	created, err := store.CreateAgent(c.Request.Context(), item)
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"agent": created, "token": raw})
}

func (s *Server) revokeAgent(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	if err := store.RevokeAgent(c.Request.Context(), c.Param("id")); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) agentHeartbeat(c *gin.Context) {
	store, agent, ok := s.agentAuth(c)
	if !ok {
		return
	}
	err := store.TouchAgent(c.Request.Context(), agent.ID)
	s.respond(c, gin.H{"ok": err == nil, "agentId": agent.ID}, err)
}

func (s *Server) agentJobs(c *gin.Context) {
	store, agent, ok := s.agentAuth(c)
	if !ok {
		return
	}
	items, err := store.AgentJobs(c.Request.Context(), *agent, atoiDefault(c.Query("limit"), 10))
	s.respond(c, items, err)
}

func (s *Server) agentSubmitResult(c *gin.Context) {
	store, agent, ok := s.agentAuth(c)
	if !ok {
		return
	}
	var result models.CheckResult
	if !bind(c, &result) {
		return
	}
	ctx := auth.WithSystemOrg(c.Request.Context(), agent.OrganizationID)
	monitor, err := s.store.GetMonitor(ctx, result.MonitorID)
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	result.OrganizationID = agent.OrganizationID
	if result.Region == "" {
		result.Region = agent.Region
	}
	if result.CheckedAt.IsZero() {
		result.CheckedAt = time.Now().UTC()
	}
	if s.monitor != nil {
		saved, err := s.monitor.RecordResult(ctx, monitor, result)
		s.respond(c, saved, err)
		return
	}
	saved, err := s.store.CreateCheckResult(ctx, result)
	s.respond(c, saved, err)
	_ = store
}

func (s *Server) agentAuth(c *gin.Context) (phase23Store, *models.Agent, bool) {
	store, ok := s.phase23(c)
	if !ok {
		return nil, nil, false
	}
	raw := bearer(c.GetHeader("Authorization"))
	if raw == "" {
		raw = strings.TrimSpace(c.GetHeader("X-Agent-Token"))
	}
	if raw == "" {
		s.unauthorized(c, "agent token required")
		return nil, nil, false
	}
	agent, err := store.FindAgentByTokenHash(c.Request.Context(), auth.Hash(raw))
	if err != nil || agent == nil {
		s.unauthorized(c, "invalid agent token")
		return nil, nil, false
	}
	return store, agent, true
}

func (s *Server) publicStatusSubscribe(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var req models.StatusPageSubscriber
	if !bind(c, &req) {
		return
	}
	confirm, err := auth.NewRawKey()
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	unsub, err := auth.NewRawKey()
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	req.StatusPageID = c.Param("slug")
	created, err := store.CreateStatusPageSubscriber(c.Request.Context(), req, auth.Hash(confirm), auth.Hash(unsub))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"subscriber": created, "confirmationToken": confirm, "unsubscribeToken": unsub})
}

func (s *Server) publicStatusConfirmSubscriber(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	item, err := store.ConfirmStatusPageSubscriber(c.Request.Context(), auth.Hash(c.Query("token")))
	s.respond(c, item, err)
}

func (s *Server) publicStatusUnsubscribe(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	if err := store.UnsubscribeStatusPageSubscriber(c.Request.Context(), auth.Hash(c.Query("token"))); err != nil {
		s.respond(c, nil, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) publicStatusAnnouncements(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	page, err := s.store.GetStatusPageBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	items, err := store.ListStatusPageAnnouncements(c.Request.Context(), page.ID, true)
	s.respond(c, items, err)
}

func (s *Server) listStatusPageSubscribers(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListStatusPageSubscribers(c.Request.Context(), c.Param("id"))
	s.respond(c, items, err)
}

func (s *Server) listStatusPageAnnouncements(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListStatusPageAnnouncements(c.Request.Context(), c.Param("id"), false)
	s.respond(c, items, err)
}

func (s *Server) createStatusPageAnnouncement(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.StatusPageAnnouncement
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	item.StatusPageID = c.Param("id")
	created, err := store.CreateStatusPageAnnouncement(c.Request.Context(), item)
	if err == nil && s.notify != nil && created.Status == "published" {
		if subscribers, subErr := store.ListStatusPageSubscribers(c.Request.Context(), c.Param("id")); subErr == nil {
			go s.notify.SendStatusPageAnnouncement(auth.WithSystemOrg(context.Background(), created.OrganizationID), subscribers, created)
		}
	}
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) listOnCallSchedules(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListOnCallSchedules(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) createOnCallSchedule(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.OnCallSchedule
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	created, err := store.CreateOnCallSchedule(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) createOnCallOverride(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.OnCallOverride
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	item.ScheduleID = c.Param("id")
	created, err := store.CreateOnCallOverride(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) resolveOnCall(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	at := time.Now().UTC()
	if raw := c.Query("at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			s.respond(c, nil, err)
			return
		}
		at = parsed
	}
	item, err := store.ResolveOnCall(c.Request.Context(), c.Param("id"), at)
	s.respond(c, item, err)
}

func (s *Server) upcomingOnCall(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.UpcomingOnCallShifts(c.Request.Context(), c.Param("id"), time.Now().UTC(), atoiDefault(c.Query("count"), 10))
	s.respond(c, items, err)
}

func (s *Server) listEscalationPolicies(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	items, err := store.ListEscalationPolicies(c.Request.Context())
	s.respond(c, items, err)
}

func (s *Server) createEscalationPolicy(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.EscalationPolicy
	if !bind(c, &item) {
		return
	}
	item.ID = ""
	created, err := store.CreateEscalationPolicy(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) createBrowserArtifact(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	var item models.BrowserArtifact
	if !bind(c, &item) {
		return
	}
	created, err := store.CreateBrowserArtifact(c.Request.Context(), item)
	s.respondStatus(c, http.StatusCreated, created, err)
}

func (s *Server) downloadBrowserArtifact(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	item, err := store.GetBrowserArtifact(c.Request.Context(), c.Param("id"))
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	if item.Path == "" {
		s.respond(c, nil, apierr.ErrNotFound)
		return
	}
	if _, err := os.Stat(item.Path); err != nil {
		s.respond(c, nil, apierr.ErrNotFound)
		return
	}
	c.File(item.Path)
}

func (s *Server) cleanupBrowserArtifacts(c *gin.Context) {
	store, ok := s.phase23(c)
	if !ok {
		return
	}
	deleted, err := store.DeleteExpiredBrowserArtifacts(c.Request.Context(), time.Now().UTC())
	s.respond(c, gin.H{"deleted": deleted}, err)
}

func parsePositiveInt(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
