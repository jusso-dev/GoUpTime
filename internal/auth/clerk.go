// Clerk session JWT verification. Clerk JWTs are standard RS256, signed by
// a per-instance key whose public half is published at
//   <issuer>/.well-known/jwks.json
//
// We fetch and cache the JWKS so verification stays networkless on the hot
// path. Refresh happens automatically when a token references an unknown
// kid, or on a slow background timer.

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// ClerkVerifier validates Clerk session JWTs and returns the embedded
// claims. Safe for concurrent use.
type ClerkVerifier struct {
	issuer string
	jwks   keyfunc.Keyfunc
}

// ClerkClaims is the subset of Clerk session-token claims we read. Field
// names match Clerk's JWT exactly (`org_id`, `org_role`, etc.) so callers
// don't need to remember an alternate naming scheme.
type ClerkClaims struct {
	Subject        string `json:"sub"`
	SessionID      string `json:"sid"`
	OrgID          string `json:"org_id"`
	OrgRole        string `json:"org_role"`
	OrgSlug        string `json:"org_slug"`
	OrgPermissions []any  `json:"org_permissions"`
	Email          string `json:"email"`
	jwt.RegisteredClaims
}

// NewClerkVerifier constructs a verifier whose JWKS is fetched lazily from
// the issuer URL and refreshed in the background. Returns an error only if
// issuer is malformed; transient JWKS fetch failures surface later on Verify.
func NewClerkVerifier(ctx context.Context, issuer string) (*ClerkVerifier, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return nil, errors.New("clerk verifier: issuer is required")
	}
	if _, err := url.Parse(issuer); err != nil {
		return nil, fmt.Errorf("clerk verifier: invalid issuer: %w", err)
	}
	jwksURL := issuer + "/.well-known/jwks.json"
	k, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("clerk verifier: load jwks: %w", err)
	}
	return &ClerkVerifier{issuer: issuer, jwks: k}, nil
}

// Verify parses raw, validates the signature against the cached JWKS, and
// returns the typed claims. Tokens whose iss does not match the configured
// issuer are rejected even if their signature checks out.
func (v *ClerkVerifier) Verify(raw string) (*ClerkClaims, error) {
	if v == nil {
		return nil, errors.New("clerk verifier: not configured")
	}
	claims := &ClerkClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, v.jwks.Keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("clerk verify: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("clerk verify: token is not valid")
	}
	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("clerk verify: issuer mismatch: got %q want %q", claims.Issuer, v.issuer)
	}
	return claims, nil
}

// ResolveRole converts Clerk's `org:` role string (e.g. "org:admin",
// "org:member") into our Role type. Unknown roles map to RoleViewer so
// users from a misconfigured Clerk instance don't accidentally get write
// access.
func ResolveRole(clerkRole string) Role {
	switch strings.ToLower(strings.TrimSpace(clerkRole)) {
	case "org:admin", "admin":
		return RoleAdmin
	case "org:owner", "owner":
		return RoleOwner
	case "org:member", "member", "basic_member":
		return RoleMember
	case "org:viewer", "viewer":
		return RoleViewer
	default:
		return RoleViewer
	}
}
