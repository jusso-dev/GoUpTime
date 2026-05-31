// Public status page handlers. Two surfaces:
//   GET /s/:slug           — SSR HTML, vanilla template, no SPA.
//   GET /s/:slug/api/summary.json — JSON for embed widgets / SDKs.
//
// Both are unauthenticated. The slug — or the custom-domain Host header
// — is the only credential needed to view the page; private/staging
// pages should set published=false in the database.

package api

import (
	"context"
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/api/web"
	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

var statusPageTemplate = template.Must(template.New("statuspage").Parse(web.StatusPageHTML))

type componentView struct {
	Name        string
	Description string
	GroupName   string
	StatusLabel string
	StatusClass string
}

type maintenanceView struct {
	Name   string
	EndsAt time.Time
}

type statusPageView struct {
	Page              models.StatusPage
	Components        []componentView
	OverallLabel      string
	OverallClass      string
	ActiveMaintenance []maintenanceView
	GeneratedAt       time.Time
}

// publicStatusPage handles GET /s/:slug. Looks up the page, renders
// every component (worst monitor status wins per component), and
// surfaces any active maintenance windows for the org.
func (s *Server) publicStatusPage(c *gin.Context) {
	ctx := auth.WithSystem(c.Request.Context())
	page, err := s.lookupPublishedPage(ctx, c)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	view, err := s.renderStatusPageView(ctx, page)
	if err != nil {
		s.logger.Error("status page render", "slug", page.Slug, "error", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "public, max-age=15")
	c.Status(http.StatusOK)
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := statusPageTemplate.Execute(c.Writer, view); err != nil {
		s.logger.Error("status page template", "slug", page.Slug, "error", err)
	}
}

func (s *Server) publicStatusPageJSON(c *gin.Context) {
	ctx := auth.WithSystem(c.Request.Context())
	page, err := s.lookupPublishedPage(ctx, c)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	view, err := s.renderStatusPageView(ctx, page)
	if err != nil {
		s.logger.Error("status page json", "slug", page.Slug, "error", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, view)
}

// lookupPublishedPage resolves the page from the :slug path param OR
// the Host header (for custom domains). Unverified custom domains and
// unpublished pages both 404.
func (s *Server) lookupPublishedPage(ctx context.Context, c *gin.Context) (models.StatusPage, error) {
	slug := c.Param("slug")
	if slug != "" {
		page, err := s.store.GetStatusPageBySlug(ctx, slug)
		if err == nil && page.Published {
			return page, nil
		}
		return models.StatusPage{}, err
	}
	host := c.Request.Host
	page, err := s.store.GetStatusPageByDomain(ctx, host)
	if err == nil && page.Published {
		return page, nil
	}
	return models.StatusPage{}, err
}

func (s *Server) renderStatusPageView(ctx context.Context, page models.StatusPage) (statusPageView, error) {
	components, err := s.store.ListStatusPageComponents(ctx, page.ID)
	if err != nil {
		return statusPageView{}, err
	}

	worstOverall := models.StatusUp
	views := make([]componentView, 0, len(components))
	for _, c := range components {
		monitors, _ := s.store.GetMonitorsByIDs(ctx, page.OrganizationID, c.MonitorIDs)
		worst := worstStatus(monitors)
		if statusRank(worst) > statusRank(worstOverall) {
			worstOverall = worst
		}
		views = append(views, componentView{
			Name:        c.Name,
			Description: c.Description,
			GroupName:   c.GroupName,
			StatusLabel: statusLabelHuman(worst),
			StatusClass: statusClass(worst),
		})
	}

	// Group components together (preserve existing position within group).
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].GroupName < views[j].GroupName
	})

	maintenance, _ := s.store.ListMaintenanceWindows(auth.WithSystemOrg(ctx, page.OrganizationID))
	now := time.Now().UTC()
	active := []maintenanceView{}
	for _, w := range maintenance {
		if w.StartsAt.Before(now) && w.EndsAt.After(now) {
			active = append(active, maintenanceView{Name: w.Name, EndsAt: w.EndsAt})
		}
	}

	return statusPageView{
		Page:              page,
		Components:        views,
		OverallLabel:      overallLabel(worstOverall, len(views)),
		OverallClass:      statusClass(worstOverall),
		ActiveMaintenance: active,
		GeneratedAt:       now,
	}, nil
}

func worstStatus(monitors []models.Monitor) models.CheckStatus {
	worst := models.StatusUp
	for _, m := range monitors {
		if statusRank(m.Status) > statusRank(worst) {
			worst = m.Status
		}
	}
	return worst
}

func statusRank(s models.CheckStatus) int {
	switch s {
	case models.StatusUp:
		return 0
	case models.StatusDegraded:
		return 1
	case models.StatusDown:
		return 2
	default:
		return 0
	}
}

func statusLabelHuman(s models.CheckStatus) string {
	switch s {
	case models.StatusUp:
		return "Operational"
	case models.StatusDegraded:
		return "Degraded"
	case models.StatusDown:
		return "Outage"
	default:
		return "Unknown"
	}
}

func statusClass(s models.CheckStatus) string {
	switch s {
	case models.StatusUp:
		return "up"
	case models.StatusDegraded:
		return "degraded"
	case models.StatusDown:
		return "down"
	default:
		return "unknown"
	}
}

func overallLabel(s models.CheckStatus, total int) string {
	if total == 0 {
		return "No components configured"
	}
	switch s {
	case models.StatusUp:
		return "All systems operational"
	case models.StatusDegraded:
		return "Some systems are experiencing issues"
	case models.StatusDown:
		return "Major outage in progress"
	default:
		return "Status unknown"
	}
}
