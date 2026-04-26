// Package oauth implements the OAuth 2.1 authorization flow endpoints.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/token"
)

const (
	authCodeLength = 32
	flowTTL        = 10 * time.Minute
)

// FlowStore abstracts database operations for OAuth pending flows.
type FlowStore interface {
	// CreatePendingFlow inserts a new pending OAuth flow.
	CreatePendingFlow(ctx context.Context, flow *PendingFlow) error

	// GetPendingFlow retrieves a pending flow by UUID.
	GetPendingFlow(ctx context.Context, uuid string) (*PendingFlow, error)

	// ConsumePendingFlow atomically marks a flow as consumed and returns it.
	ConsumePendingFlow(ctx context.Context, uuid string) (*PendingFlow, error)

	// GetTenantOAuth retrieves OAuth-specific tenant configuration.
	GetTenantOAuth(ctx context.Context, tenantID string) (*TenantOAuthConfig, error)
}

// SessionStore abstracts session operations needed for refresh token exchange.
type SessionStore interface {
	// GetSession retrieves a session by ID.
	GetSession(ctx context.Context, id string) (*SessionRecord, error)

	// RefreshSession updates expiry, scopes, and increments refresh_count.
	RefreshSession(ctx context.Context, id string, expiresAt time.Time, scopes []string) error

	// GetTenantPolicy retrieves a tenant's session policy.
	GetTenantPolicy(ctx context.Context, tenantID string) (*TenantPolicy, error)
}

// PendingFlow represents an OAuth pending flow record.
type PendingFlow struct {
	UUID                string
	TenantID            string
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	AuthCode            string
	SessionID           string
	Status              string // "pending", "connected", "expired", "consumed"
	ExpiresAt           time.Time
}

// TenantOAuthConfig holds OAuth-specific tenant configuration.
type TenantOAuthConfig struct {
	ID               string
	RedirectURIs     []string
	MerchantLoginURL string
}

// SessionRecord holds session fields needed by OAuth token exchange.
type SessionRecord struct {
	ID           string
	TenantID     string
	Status       session.Status
	Scopes       []string
	RefreshCount int
	ExpiresAt    time.Time
}

// TenantPolicy holds session policy constraints.
type TenantPolicy struct {
	SessionMaxExpiry time.Duration
	MaxRefreshes     int
}

// Handler handles OAuth 2.1 HTTP requests.
type Handler struct {
	flowStore    FlowStore
	sessionStore SessionStore
	oauthCache   *cache.OAuthCache
	sessionCache *cache.SessionCache
	keyLookup    func(ctx context.Context, tenantID string) (crypto.TenantKeys, error)
	issuer       *token.Issuer
	nonces       *cache.NonceStore
}

// NewHandler creates an OAuth Handler with the given dependencies.
func NewHandler(
	flowStore FlowStore,
	sessionStore SessionStore,
	oauthCache *cache.OAuthCache,
	sessionCache *cache.SessionCache,
	keyLookup func(ctx context.Context, tenantID string) (crypto.TenantKeys, error),
	issuer *token.Issuer,
	nonces *cache.NonceStore,
) *Handler {
	return &Handler{
		flowStore:    flowStore,
		sessionStore: sessionStore,
		oauthCache:   oauthCache,
		sessionCache: sessionCache,
		keyLookup:    keyLookup,
		issuer:       issuer,
		nonces:       nonces,
	}
}

// nonceCheck returns a DPoP nonce checker function.
// If the nonce store is nil, it returns nil (nonce check is skipped).
func (h *Handler) nonceCheck(ctx context.Context) func(jti string) (bool, error) {
	if h.nonces == nil {
		return nil
	}
	return func(jti string) (bool, error) {
		return h.nonces.CheckDPoPNonce(ctx, jti)
	}
}

// Metadata handles GET /.well-known/oauth-authorization-server.
// It returns the OAuth 2.1 authorization server metadata document.
func (h *Handler) Metadata(w http.ResponseWriter, _ *http.Request) {
	issuer := h.issuer.Config().Issuer

	meta := map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"dpop_signing_alg_values_supported":     []string{"EdDSA"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meta)
}

// generateAuthCode creates a cryptographically random hex-encoded authorization code.
func generateAuthCode() (string, error) {
	b := make([]byte, authCodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateUUID creates a cryptographically random hex-encoded UUID-like identifier.
func generateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// verifyPKCE validates a code_verifier against a code_challenge using S256.
func verifyPKCE(verifier, challenge string) bool {
	hash := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(hash[:])
	return computed == challenge
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
