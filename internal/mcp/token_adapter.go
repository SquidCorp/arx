package mcp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/token"
	"github.com/golang-jwt/jwt/v5"
)

// SessionReader looks up a session for token validation.
type SessionReader interface {
	GetSession(ctx context.Context, id string) (*token.SessionInfo, error)
}

// TenantKeyLookup resolves a tenant's cryptographic keys.
type TenantKeyLookup func(ctx context.Context, tenantID string) (crypto.TenantKeys, error)

// TokenExtractorAdapter validates DPoP-bound access tokens and returns MCP-compatible
// session info. It reuses the existing token validation logic.
type TokenExtractorAdapter struct {
	keyLookup     TenantKeyLookup
	sessionReader SessionReader
	issuer        string
	audience      string
}

// NewTokenExtractorAdapter creates a TokenExtractorAdapter.
func NewTokenExtractorAdapter(
	keyLookup TenantKeyLookup,
	sessionReader SessionReader,
	issuer, audience string,
) *TokenExtractorAdapter {
	return &TokenExtractorAdapter{
		keyLookup:     keyLookup,
		sessionReader: sessionReader,
		issuer:        issuer,
		audience:      audience,
	}
}

// ExtractToken validates the access token on the request and returns session info.
// It does NOT validate the DPoP proof here — that's the responsibility of individual
// tools (e.g., call_tool requires DPoP, list_tools does not).
func (a *TokenExtractorAdapter) ExtractToken(r *http.Request) (*TokenInfo, error) {
	tokenStr, ok := extractBearerToken(r)
	if !ok {
		return nil, ErrNoToken
	}

	// Parse unverified to extract tenant.
	unverified, _, err := new(jwt.Parser).ParseUnverified(tokenStr, &token.AccessTokenClaims{})
	if err != nil {
		return nil, ErrNoToken
	}

	claims, ok := unverified.Claims.(*token.AccessTokenClaims)
	if !ok || claims.Tenant == "" {
		return nil, ErrNoToken
	}

	// Look up tenant keys.
	keys, err := a.keyLookup(r.Context(), claims.Tenant)
	if err != nil {
		return nil, ErrNoToken
	}

	// Verify signature.
	verified, err := token.ParseAccessToken(tokenStr, keys.ArxSigningPublicKey)
	if err != nil {
		return nil, ErrNoToken
	}

	// Check issuer and audience.
	if verified.Issuer != a.issuer {
		return nil, ErrNoToken
	}
	foundAud := false
	for _, aud := range verified.Audience {
		if aud == a.audience {
			foundAud = true
			break
		}
	}
	if !foundAud {
		return nil, ErrNoToken
	}

	// Look up session.
	sess, err := a.sessionReader.GetSession(r.Context(), verified.Subject)
	if err != nil {
		return nil, ErrNoToken
	}

	// Check session is valid.
	if sess.ExpiresAt.Before(time.Now()) || sess.Status != session.StatusActive {
		return nil, ErrNoToken
	}

	return &TokenInfo{
		SessionID: sess.ID,
		TenantID:  sess.TenantID,
		UserID:    sess.UserID,
		Scopes:    sess.Scopes,
		ExpiresAt: sess.ExpiresAt,
		Status:    string(sess.Status),
	}, nil
}

// extractBearerToken extracts the token from "DPoP <token>" or "Bearer <token>".
func extractBearerToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	for _, prefix := range []string{"DPoP ", "Bearer "} {
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(auth[len(prefix):]), true
		}
	}
	return "", false
}
