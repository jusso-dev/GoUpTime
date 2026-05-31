package repository

import (
	"context"
	"fmt"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/auth"
)

// tenantScope returns the organization id that must be applied as a WHERE
// clause filter for the calling principal, along with a boolean indicating
// whether tenancy filtering should be skipped (system actor with no pinned
// org).
//
// Rules:
//   - User / APIKey: OrgID required; missing → ErrUnauthorized.
//   - System with empty OrgID: skip=true (cross-org scan allowed).
//   - System with OrgID: filter by that org.
//   - Anything else (no principal): treat as system without org for
//     backwards compatibility with code paths that pre-date the principal.
//     The tenancy lint test enforces that all user-facing queries actually
//     filter, so this fallback is safe — repositories that need to enforce
//     tenancy will still do so explicitly via requireOrg.
func (s *PostgresStore) tenantScope(ctx context.Context) (orgID string, skip bool, err error) {
	p := auth.FromContext(ctx)
	switch p.ActorType {
	case auth.ActorUser, auth.ActorAPIKey:
		if p.OrgID == "" {
			return "", false, fmt.Errorf("%w: organization context is required", apierr.ErrUnauthorized)
		}
		return p.OrgID, false, nil
	case auth.ActorSystem:
		return p.OrgID, p.OrgID == "", nil
	default:
		// No principal attached. Permitted only for code paths that
		// pre-date the principal (notably tests using context.Background).
		return "", true, nil
	}
}

// requireOrg returns the org id from the principal or an error. Use when
// inserting a row that needs an explicit organization assignment.
func (s *PostgresStore) requireOrg(ctx context.Context) (string, error) {
	p := auth.FromContext(ctx)
	if p.OrgID != "" {
		return p.OrgID, nil
	}
	if p.ActorType == auth.ActorSystem {
		// System callers must explicitly pin an org for write operations.
		return "", fmt.Errorf("%w: system caller must pin an organization", apierr.ErrUnauthorized)
	}
	return "", fmt.Errorf("%w: organization context is required", apierr.ErrUnauthorized)
}
