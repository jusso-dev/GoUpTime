package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestClerkVerifierAcceptsValidJWT spins up a fake JWKS server with an
// RSA key under our control, signs a token with the matching private key,
// and verifies that NewClerkVerifier accepts it. This exercises the
// keyfunc + jwt parse path without needing a real Clerk instance.
func TestClerkVerifierAcceptsValidJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-key"

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks(key, kid))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := NewClerkVerifier(ctx, srv.URL)
	if err != nil {
		t.Fatalf("NewClerkVerifier: %v", err)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":      "user_abc",
		"iss":      srv.URL,
		"sid":      "sess_123",
		"org_id":   "org_xyz",
		"org_role": "org:admin",
		"email":    "test@example.com",
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = kid
	raw, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	claims, err := v.Verify(raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user_abc" {
		t.Fatalf("subject mismatch: %q", claims.Subject)
	}
	if claims.OrgID != "org_xyz" {
		t.Fatalf("org id mismatch: %q", claims.OrgID)
	}
	if ResolveRole(claims.OrgRole) != RoleAdmin {
		t.Fatalf("resolved role mismatch: %v", ResolveRole(claims.OrgRole))
	}
}

// TestClerkVerifierRejectsIssuerMismatch ensures we don't accept a
// validly-signed token from the wrong issuer — important when a single
// JWKS endpoint signs tokens for multiple tenants.
func TestClerkVerifierRejectsIssuerMismatch(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	const kid = "test-key"
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks(key, kid))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, _ := NewClerkVerifier(ctx, srv.URL)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "u", "iss": "https://other.example",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = kid
	raw, _ := tok.SignedString(key)

	if _, err := v.Verify(raw); err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("expected issuer mismatch error, got %v", err)
	}
}

func TestRoleHierarchy(t *testing.T) {
	cases := []struct {
		role Role
		need Role
		ok   bool
	}{
		{RoleOwner, RoleAdmin, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleMember, RoleAdmin, false},
		{RoleViewer, RoleMember, false},
		{RoleMember, RoleViewer, true},
		{"", RoleViewer, false},
	}
	for _, c := range cases {
		if got := c.role.AtLeast(c.need); got != c.ok {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", c.role, c.need, got, c.ok)
		}
	}
}

// jwks builds a single-key JWKS payload from an RSA public key, matching
// the on-the-wire format the keyfunc loader expects.
func jwks(key *rsa.PrivateKey, kid string) map[string]any {
	n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": kid,
			"n":   n,
			"e":   e,
		}},
	}
}
