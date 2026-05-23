package auth

import (
	"context"
	"errors"
)

// Role enumerates the membership roles a user can hold within an
// organization. Ordering encodes privilege: an operation that requires
// RoleMember is also satisfied by RoleAdmin and RoleOwner.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// AtLeast reports whether r has at least the privilege level of need.
func (r Role) AtLeast(need Role) bool {
	return roleRank(r) >= roleRank(need)
}

func roleRank(r Role) int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleMember:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// ActorType describes what kind of credential authenticated the request,
// which determines how the principal can be used by the repository.
type ActorType string

const (
	// ActorUser is a Clerk-authenticated end user. Must carry an OrgID.
	ActorUser ActorType = "user"
	// ActorAPIKey is a machine credential. Always org-scoped.
	ActorAPIKey ActorType = "apiKey"
	// ActorSystem is an internal caller (scheduler, worker, webhook handler).
	// Allowed to read across orgs when OrgID is empty; otherwise pinned.
	ActorSystem ActorType = "system"
)

// Principal carries the authenticated identity for a request. It is the
// single source of truth for tenancy: every repository call inspects the
// principal from context before issuing SQL.
type Principal struct {
	ActorType ActorType
	UserID    string
	OrgID     string
	Role      Role
	// APIKeyID is set when the request authenticated via an API key. Used
	// for audit logging.
	APIKeyID string
	// ClerkSessionID is set when the request authenticated via a Clerk JWT.
	// Used for revocation and audit logging.
	ClerkSessionID string
}

// IsZero reports whether p was returned from FromContext with no principal
// attached. Useful as a sanity check at trust boundaries.
func (p Principal) IsZero() bool {
	return p.ActorType == "" && p.UserID == "" && p.OrgID == "" && p.APIKeyID == ""
}

type principalKey struct{}

// WithPrincipal returns a copy of ctx that carries p. Repository methods read
// it via FromContext; handlers populate it via the auth middleware.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext extracts the Principal previously stored by WithPrincipal.
// Returns a zero-value Principal when none is present — callers that need
// authorization should call Require instead.
func FromContext(ctx context.Context) Principal {
	if v, ok := ctx.Value(principalKey{}).(Principal); ok {
		return v
	}
	return Principal{}
}

// ErrMissingPrincipal is returned by Require when no principal is attached
// to the context. Translated to 401 by apierr.StatusFor via ErrUnauthorized.
var ErrMissingPrincipal = errors.New("auth: principal missing from context")

// ErrInsufficientRole is returned when the caller's role is below what the
// operation requires.
var ErrInsufficientRole = errors.New("auth: insufficient role")

// Require returns the Principal from ctx or an error suitable for handlers.
func Require(ctx context.Context) (Principal, error) {
	p := FromContext(ctx)
	if p.IsZero() {
		return Principal{}, ErrMissingPrincipal
	}
	return p, nil
}

// WithSystem returns a context whose principal authorizes cross-org reads.
// Use for scheduler/worker code that legitimately needs to scan every
// organization (e.g., enumerating monitors that are due).
func WithSystem(ctx context.Context) context.Context {
	return WithPrincipal(ctx, Principal{ActorType: ActorSystem})
}

// WithSystemOrg returns a context whose principal is a system actor pinned
// to a single organization. Use when a background job knows which org it is
// operating on (e.g., persisting a check result for a known monitor).
func WithSystemOrg(ctx context.Context, orgID string) context.Context {
	return WithPrincipal(ctx, Principal{ActorType: ActorSystem, OrgID: orgID})
}
