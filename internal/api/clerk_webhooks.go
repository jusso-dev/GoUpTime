// Clerk webhook handler. Clerk fires webhooks via Svix; we verify the Svix
// signature (HMAC-SHA256 with the per-instance signing secret), dedup by
// event id, and mirror user/organization/membership changes into our local
// tables so the request hot path never has to call out to Clerk.
//
// Events handled:
//   user.created / user.updated / user.deleted
//   organization.created / organization.updated / organization.deleted
//   organizationMembership.created / .updated / .deleted

package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

// maxClerkWebhookBody caps how much we'll read from a single webhook
// delivery. Clerk payloads are well under 50 KiB in practice; the cap
// defends against bad receivers spraying huge bodies.
const maxClerkWebhookBody = 64 << 10

// clerkWebhookTolerance is the maximum allowed skew between the Svix
// timestamp header and our clock. 5 minutes matches Svix's documented
// recommendation.
const clerkWebhookTolerance = 5 * time.Minute

type clerkEvent struct {
	Type   string          `json:"type"`
	Object string          `json:"object"`
	Data   json.RawMessage `json:"data"`
}

type clerkUserData struct {
	ID             string `json:"id"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	ImageURL       string `json:"image_url"`
	EmailAddresses []struct {
		EmailAddress string `json:"email_address"`
		ID           string `json:"id"`
	} `json:"email_addresses"`
	PrimaryEmailAddressID string `json:"primary_email_address_id"`
}

type clerkOrganizationData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type clerkMembershipData struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	Organization struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"organization"`
	PublicUserData struct {
		UserID string `json:"user_id"`
	} `json:"public_user_data"`
}

func (s *Server) clerkWebhook(c *gin.Context) {
	if s.cfg.ClerkWebhookSecret == "" {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error":     "clerk webhook secret is not configured",
			"requestId": c.GetString(requestIDKey),
		})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxClerkWebhookBody+1))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error(), "requestId": c.GetString(requestIDKey)})
		return
	}
	if int64(len(body)) > maxClerkWebhookBody {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "webhook body too large", "requestId": c.GetString(requestIDKey)})
		return
	}

	svixID := c.GetHeader("svix-id")
	svixTimestamp := c.GetHeader("svix-timestamp")
	svixSignature := c.GetHeader("svix-signature")
	if svixID == "" || svixTimestamp == "" || svixSignature == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing svix headers", "requestId": c.GetString(requestIDKey)})
		return
	}
	if err := verifySvixSignature(s.cfg.ClerkWebhookSecret, svixID, svixTimestamp, svixSignature, body); err != nil {
		s.logger.Warn("clerk webhook signature failed",
			"request_id", c.GetString(requestIDKey),
			"svix_id", svixID,
			"error", err,
		)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid signature", "requestId": c.GetString(requestIDKey)})
		return
	}

	// Dedup. Once recorded, we always return 200 so Svix stops retrying.
	systemCtx := auth.WithSystem(c.Request.Context())
	fresh, err := s.store.RecordWebhookEvent(systemCtx, svixID, "clerk", body)
	if err != nil {
		s.logger.Error("clerk webhook dedup failed",
			"request_id", c.GetString(requestIDKey),
			"svix_id", svixID,
			"error", err,
		)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "dedup failed", "requestId": c.GetString(requestIDKey)})
		return
	}
	if !fresh {
		c.JSON(http.StatusOK, gin.H{"deduped": true})
		return
	}

	var event clerkEvent
	if err := json.Unmarshal(body, &event); err != nil {
		// Already deduped — log and accept so we don't keep getting retries
		// for a malformed payload.
		s.logger.Warn("clerk webhook decode failed",
			"request_id", c.GetString(requestIDKey),
			"svix_id", svixID,
			"error", err,
		)
		c.JSON(http.StatusOK, gin.H{"accepted": true, "warning": "decode failed"})
		return
	}

	if err := s.applyClerkEvent(systemCtx, event); err != nil {
		s.logger.Error("clerk webhook apply failed",
			"request_id", c.GetString(requestIDKey),
			"svix_id", svixID,
			"event_type", event.Type,
			"error", err,
		)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "apply failed", "requestId": c.GetString(requestIDKey)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"applied": true, "type": event.Type})
}

// applyClerkEvent dispatches a verified Clerk event to the right repository
// upsert. Unknown event types are silently ignored — Clerk adds new event
// types over time and we don't want a "what's that?" event to 500 the
// webhook endpoint.
func (s *Server) applyClerkEvent(ctx context.Context, event clerkEvent) error {
	switch event.Type {
	case "user.created", "user.updated":
		var u clerkUserData
		if err := json.Unmarshal(event.Data, &u); err != nil {
			return fmt.Errorf("decode user data: %w", err)
		}
		_, err := s.store.UpsertUser(ctx, models.User{
			ClerkUserID: u.ID,
			Email:       primaryEmail(u),
			Name:        strings.TrimSpace(u.FirstName + " " + u.LastName),
			ImageURL:    u.ImageURL,
		})
		return err

	case "user.deleted":
		var u clerkUserData
		if err := json.Unmarshal(event.Data, &u); err != nil {
			return fmt.Errorf("decode user data: %w", err)
		}
		return s.store.DeleteUserByClerkID(ctx, u.ID)

	case "organization.created", "organization.updated":
		var o clerkOrganizationData
		if err := json.Unmarshal(event.Data, &o); err != nil {
			return fmt.Errorf("decode org data: %w", err)
		}
		_, err := s.store.UpsertOrganization(ctx, models.Organization{
			ClerkOrgID: o.ID,
			Name:       o.Name,
			Slug:       o.Slug,
			Plan:       "free",
		})
		return err

	case "organization.deleted":
		var o clerkOrganizationData
		if err := json.Unmarshal(event.Data, &o); err != nil {
			return fmt.Errorf("decode org data: %w", err)
		}
		return s.store.DeleteOrganizationByClerkID(ctx, o.ID)

	case "organizationMembership.created", "organizationMembership.updated":
		var m clerkMembershipData
		if err := json.Unmarshal(event.Data, &m); err != nil {
			return fmt.Errorf("decode membership data: %w", err)
		}
		org, err := s.store.GetOrganizationByClerkID(ctx, m.Organization.ID)
		if err != nil {
			// The org webhook may not have arrived yet — upsert it on the
			// fly so the membership row has a target.
			org, err = s.store.UpsertOrganization(ctx, models.Organization{
				ClerkOrgID: m.Organization.ID,
				Name:       m.Organization.Name,
				Slug:       m.Organization.Slug,
				Plan:       "free",
			})
			if err != nil {
				return fmt.Errorf("ensure organization: %w", err)
			}
		}
		user, err := s.store.GetUserByClerkID(ctx, m.PublicUserData.UserID)
		if err != nil {
			// Same idea — synthesize a placeholder if the user webhook
			// is in flight; email backfills on user.created.
			user, err = s.store.UpsertUser(ctx, models.User{
				ClerkUserID: m.PublicUserData.UserID,
				Email:       "",
			})
			if err != nil {
				return fmt.Errorf("ensure user: %w", err)
			}
		}
		return s.store.UpsertMembership(ctx, models.Membership{
			OrganizationID: org.ID,
			UserID:         user.ID,
			Role:           string(auth.ResolveRole(m.Role)),
		})

	case "organizationMembership.deleted":
		var m clerkMembershipData
		if err := json.Unmarshal(event.Data, &m); err != nil {
			return fmt.Errorf("decode membership data: %w", err)
		}
		org, err := s.store.GetOrganizationByClerkID(ctx, m.Organization.ID)
		if err != nil {
			// Membership for an unknown org — nothing to delete.
			return nil
		}
		user, err := s.store.GetUserByClerkID(ctx, m.PublicUserData.UserID)
		if err != nil {
			return nil
		}
		return s.store.DeleteMembership(ctx, org.ID, user.ID)
	}
	return nil
}

func primaryEmail(u clerkUserData) string {
	for _, addr := range u.EmailAddresses {
		if addr.ID == u.PrimaryEmailAddressID {
			return addr.EmailAddress
		}
	}
	if len(u.EmailAddresses) > 0 {
		return u.EmailAddresses[0].EmailAddress
	}
	return ""
}

// verifySvixSignature implements the Svix webhook verification described at
// https://docs.svix.com/receiving/verifying-payloads/how-manual — namely
//
//   signedContent = svix-id + "." + svix-timestamp + "." + body
//   signature = base64(HMAC-SHA256(secret, signedContent))
//
// The svix-signature header is a space-separated list of "v1,base64sig"
// values; any one matching constitutes a valid signature.
func verifySvixSignature(secret, svixID, svixTimestamp, svixSignature string, body []byte) error {
	// Reject stale or clock-skewed deliveries.
	ts, err := strconv.ParseInt(svixTimestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	delivered := time.Unix(ts, 0)
	if abs(time.Since(delivered)) > clerkWebhookTolerance {
		return fmt.Errorf("timestamp outside tolerance: %s", delivered)
	}

	// The secret arrives as "whsec_<base64>"; strip the prefix and decode.
	raw := strings.TrimPrefix(secret, "whsec_")
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("decode webhook secret: %w", err)
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(svixID))
	mac.Write([]byte("."))
	mac.Write([]byte(svixTimestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for _, sig := range strings.Split(svixSignature, " ") {
		sig = strings.TrimSpace(sig)
		// Each entry is "v1,<base64>". Older versions used "v0".
		parts := strings.SplitN(sig, ",", 2)
		if len(parts) != 2 {
			continue
		}
		if hmac.Equal([]byte(parts[1]), []byte(expected)) {
			return nil
		}
	}
	return errors.New("no matching signature")
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
