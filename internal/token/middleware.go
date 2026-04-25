package token

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/golang-jwt/jwt/v5"
)

// Context keys for storing validated token data.
type ctxKey string

const (
	// ctxSessionID is the context key for the validated session ID.
	ctxSessionID ctxKey = "token_session_id"
	// ctxTenantID is the context key for the validated tenant ID.
	ctxTenantID ctxKey = "token_tenant_id"
	// ctxScopes is the context key for the validated scopes.
	ctxScopes ctxKey = "token_scopes"
)

// SessionReader looks up a session by ID.
type SessionReader interface {
	GetSession(ctx context.Context, id string) (*SessionInfo, error)
}

// SessionInfo holds the fields needed by the token middleware.
type SessionInfo struct {
	ID        string
	TenantID  string
	Status    session.Status
	Scopes    []string
	ExpiresAt time.Time
}

// TenantKeyLookup resolves a tenant's cryptographic keys by tenant ID.
type TenantKeyLookup func(ctx context.Context, tenantID string) (crypto.TenantKeys, error)

// Validator is HTTP middleware that validates DPoP-bound access tokens.
type Validator struct {
	keyLookup     TenantKeyLookup
	sessionReader SessionReader
	issuer        string
	audience      string
	nonceCheck    func(jti string) (bool, error)
}

// NewValidator creates access token validation middleware.
func NewValidator(keyLookup TenantKeyLookup, sessionReader SessionReader, issuer, audience string, nonceCheck func(jti string) (bool, error)) *Validator {
	return &Validator{
		keyLookup:     keyLookup,
		sessionReader: sessionReader,
		issuer:        issuer,
		audience:      audience,
		nonceCheck:    nonceCheck,
	}
}

// Middleware returns an http.Handler that validates access tokens.
// On success it stores session ID, tenant ID, and scopes in the request context.
func (v *Validator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr, ok := extractToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing_token")
			return
		}

		// Parse without verifying to extract tenant claim.
		unverified, _, err := new(jwt.Parser).ParseUnverified(tokenStr, &AccessTokenClaims{})
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_token")
			return
		}

		unverifiedClaims, ok := unverified.Claims.(*AccessTokenClaims)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_token")
			return
		}

		if unverifiedClaims.Tenant == "" {
			writeError(w, http.StatusUnauthorized, "invalid_token")
			return
		}

		// Look up tenant keys to verify signature.
		keys, err := v.keyLookup(r.Context(), unverifiedClaims.Tenant)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unknown_tenant")
			return
		}

		// Parse and verify the token.
		claims, err := ParseAccessToken(tokenStr, keys.ArxSigningPublicKey)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_token")
			return
		}

		// Validate issuer.
		if claims.Issuer != v.issuer {
			writeError(w, http.StatusUnauthorized, "invalid_issuer")
			return
		}

		// Validate audience.
		foundAud := false
		for _, aud := range claims.Audience {
			if aud == v.audience {
				foundAud = true
				break
			}
		}
		if !foundAud {
			writeError(w, http.StatusUnauthorized, "invalid_audience")
			return
		}

		// Look up session.
		sess, err := v.sessionReader.GetSession(r.Context(), claims.Subject)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session_not_found")
			return
		}

		// Check session expiry.
		if sess.ExpiresAt.Before(NowFunc()) {
			writeError(w, http.StatusUnauthorized, "session_expired")
			return
		}

		// Check session status.
		switch sess.Status {
		case session.StatusActive:
			// ok
		case session.StatusExpired:
			writeError(w, http.StatusUnauthorized, "session_expired")
			return
		case session.StatusRevoked:
			writeError(w, http.StatusUnauthorized, "session_revoked")
			return
		case session.StatusSuspended:
			writeError(w, http.StatusForbidden, "session_suspended")
			return
		default:
			writeError(w, http.StatusUnauthorized, "invalid_session")
			return
		}

		// Validate DPoP proof on the resource request (RFC 9449).
		dpopProof := r.Header.Get("DPoP")
		if dpopProof == "" {
			writeError(w, http.StatusUnauthorized, "missing_dpop_proof")
			return
		}

		thumbprint, err := ValidateDPoPProof(dpopProof, tokenStr, r.Method, RequestURL(r), v.nonceCheck)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_dpop_proof")
			return
		}

		if thumbprint != claims.Cnf.JKT {
			writeError(w, http.StatusUnauthorized, "dpop_binding_mismatch")
			return
		}

		// Store in context.
		ctx := r.Context()
		ctx = context.WithValue(ctx, ctxSessionID, claims.Subject)
		ctx = context.WithValue(ctx, ctxTenantID, claims.Tenant)
		ctx = context.WithValue(ctx, ctxScopes, strings.Fields(claims.Scope))

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractToken extracts the token from the Authorization header.
// Expected format: "DPoP <token>"
func extractToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	const prefix = "DPoP "
	if !strings.HasPrefix(auth, prefix) {
		return "", false
	}
	return strings.TrimSpace(auth[len(prefix):]), true
}

// SessionIDFromContext extracts the validated session ID from the context.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxSessionID).(string)
	return id, ok
}

// TenantIDFromContext extracts the validated tenant ID from the context.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxTenantID).(string)
	return id, ok
}

// ScopesFromContext extracts the validated scopes from the context.
func ScopesFromContext(ctx context.Context) ([]string, bool) {
	scopes, ok := ctx.Value(ctxScopes).([]string)
	return scopes, ok
}

// writeError writes a JSON error response.
// code should be a known constant; arbitrary strings are escaped via json.Marshal.
func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp, _ := json.Marshal(map[string]string{"error": code})
	//nolint:errcheck
	w.Write(resp)
}

// Ensure Validator implements the expected interface at compile time.
var _ interface {
	Middleware(next http.Handler) http.Handler
} = (*Validator)(nil)
