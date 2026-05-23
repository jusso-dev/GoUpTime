// Mobile-app-ready identity endpoints. /api/v1/me returns the current
// user, their organizations, and the active org so the app can render its
// org picker and role-aware UI in a single round-trip.

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

type meResponse struct {
	User                meUser                    `json:"user"`
	Organizations       []models.MembershipDetail `json:"organizations"`
	CurrentOrganization *models.MembershipDetail  `json:"currentOrganization,omitempty"`
	Role                auth.Role                 `json:"role"`
	ActorType           auth.ActorType            `json:"actorType"`
}

type meUser struct {
	ID          string `json:"id"`
	ClerkUserID string `json:"clerkUserId,omitempty"`
	Email       string `json:"email,omitempty"`
	Name        string `json:"name,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

func (s *Server) me(c *gin.Context) {
	p, err := auth.Require(c.Request.Context())
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	resp := meResponse{
		ActorType: p.ActorType,
		Role:      p.Role,
	}

	// We need cross-org reads to hydrate the org list — the user's
	// memberships span multiple tenants by definition.
	sysCtx := auth.WithSystem(c.Request.Context())

	if p.UserID != "" {
		user, err := s.store.GetUserByID(sysCtx, p.UserID)
		if err == nil {
			resp.User = meUser{
				ID:          user.ID,
				ClerkUserID: user.ClerkUserID,
				Email:       user.Email,
				Name:        user.Name,
				ImageURL:    user.ImageURL,
			}
		}
		memberships, err := s.store.ListMembershipsForUser(sysCtx, p.UserID)
		if err == nil {
			resp.Organizations = memberships
			for i := range memberships {
				if memberships[i].OrganizationID == p.OrgID {
					resp.CurrentOrganization = &resp.Organizations[i]
					break
				}
			}
		}
	} else {
		// API-key actor — return a synthetic identity so the response
		// shape is stable for the mobile client even when CI hits /me.
		resp.User = meUser{ID: "apikey:" + p.APIKeyID}
	}

	// Surface a current-org stub when the principal is org-pinned but no
	// matching membership row exists (e.g. API-key actor, or user whose
	// membership webhook hasn't arrived yet).
	if resp.CurrentOrganization == nil && p.OrgID != "" {
		if org, err := s.store.GetOrganization(sysCtx, p.OrgID); err == nil {
			resp.CurrentOrganization = &models.MembershipDetail{
				OrganizationID:   org.ID,
				OrganizationName: org.Name,
				OrganizationSlug: org.Slug,
				Plan:             org.Plan,
				Role:             string(p.Role),
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (s *Server) listOrganizations(c *gin.Context) {
	p, err := auth.Require(c.Request.Context())
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	sysCtx := auth.WithSystem(c.Request.Context())
	if p.UserID == "" {
		// Machine clients see only the org their key is pinned to.
		if p.OrgID == "" {
			c.JSON(http.StatusOK, []models.MembershipDetail{})
			return
		}
		org, err := s.store.GetOrganization(sysCtx, p.OrgID)
		if err != nil {
			s.respond(c, nil, err)
			return
		}
		c.JSON(http.StatusOK, []models.MembershipDetail{{
			OrganizationID:   org.ID,
			OrganizationName: org.Name,
			OrganizationSlug: org.Slug,
			Plan:             org.Plan,
			Role:             string(p.Role),
		}})
		return
	}
	memberships, err := s.store.ListMembershipsForUser(sysCtx, p.UserID)
	if err != nil {
		s.respond(c, nil, err)
		return
	}
	c.JSON(http.StatusOK, memberships)
}
