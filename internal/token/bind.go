package token

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
)

// BindSessionStore abstracts database operations needed by the bind handler.
type BindSessionStore interface {
	// GetAndConsumeBindCode atomically looks up a session by its bind code,
	// validates that it has not expired, and consumes it. If the bind code
	// does not exist, has already been used, or is expired, an error is returned.
	GetAndConsumeBindCode(ctx context.Context, bindCode string) (*BindSession, error)
}

// BindSession holds the session fields needed for bind-code exchange.
type BindSession struct {
	ID              string
	TenantID        string
	Status          session.Status
	Scopes          []string
	BindCodeExpires *time.Time
	ExpiresAt       time.Time
}

// TenantKeyLookup resolves a tenant's cryptographic keys by tenant ID.
// This type alias shadows the one in middleware.go but keeps the package self-contained.

// BindHandler handles POST /token/bind.
type BindHandler struct {
	sessions     BindSessionStore
	sessionCache *cache.SessionCache
	keyLookup    func(ctx context.Context, tenantID string) (crypto.TenantKeys, error)
	issuer       *Issuer
	nonces       *cache.NonceStore
}

// NewBindHandler creates a BindHandler with the given dependencies.
func NewBindHandler(
	sessions BindSessionStore,
	sessionCache *cache.SessionCache,
	keyLookup func(ctx context.Context, tenantID string) (crypto.TenantKeys, error),
	issuer *Issuer,
	nonces *cache.NonceStore,
) *BindHandler {
	return &BindHandler{
		sessions:     sessions,
		sessionCache: sessionCache,
		keyLookup:    keyLookup,
		issuer:       issuer,
		nonces:       nonces,
	}
}

// bindRequest is the JSON payload for POST /token/bind.
type bindRequest struct {
	BindCode  string `json:"bind_code"`
	DPoPProof string `json:"dpop_proof"`
}

// bindResponse is the JSON response for a successful bind.
type bindResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// Bind handles POST /token/bind.
// It validates the bind-code and DPoP proof, then issues a DPoP-bound access token.
func (h *BindHandler) Bind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	var req bindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}

	if req.BindCode == "" {
		writeError(w, http.StatusBadRequest, "missing_bind_code")
		return
	}
	if req.DPoPProof == "" {
		writeError(w, http.StatusBadRequest, "missing_dpop_proof")
		return
	}

	// Atomically consume the bind code and retrieve the session.
	sess, err := h.sessions.GetAndConsumeBindCode(r.Context(), req.BindCode)
	if err != nil {
		switch {
		case errors.Is(err, ErrBindCodeNotFound):
			writeError(w, http.StatusBadRequest, "invalid_bind_code")
		case errors.Is(err, ErrBindCodeAlreadyUsed):
			writeError(w, http.StatusBadRequest, "bind_code_already_used")
		case errors.Is(err, ErrBindCodeExpired):
			writeError(w, http.StatusBadRequest, "bind_code_expired")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}

	// Check session is active.
	if sess.Status != session.StatusActive {
		writeError(w, http.StatusForbidden, "session_not_active")
		return
	}

	// Validate DPoP proof.
	thumbprint, err := ValidateDPoPProof(req.DPoPProof, "", r.Method, r.URL.String(), func(jti string) (bool, error) {
		return h.nonces.CheckDPoPNonce(r.Context(), jti)
	})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_dpop_proof")
		return
	}

	// Look up tenant keys for signing.
	keys, err := h.keyLookup(r.Context(), sess.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Issue access token.
	accessToken, err := h.issuer.IssueAccessToken(r.Context(), keys, sess.ID, sess.TenantID, thumbprint, sess.Scopes, sess.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Cache the session for the hot path.
	remaining := time.Until(sess.ExpiresAt)
	if remaining > 0 {
		_ = h.sessionCache.Set(r.Context(), sess.ID, &cache.SessionData{
			TenantID:  sess.TenantID,
			Status:    string(sess.Status),
			Scopes:    sess.Scopes,
			ExpiresAt: sess.ExpiresAt,
		}, remaining)
	}

	expiresIn := int64(time.Until(sess.ExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(bindResponse{
		AccessToken: accessToken,
		TokenType:   "DPoP",
		ExpiresIn:   expiresIn,
	})
}

// Bind-code errors.
var (
	// ErrBindCodeNotFound is returned when a bind code lookup fails.
	ErrBindCodeNotFound = errors.New("bind code not found")
	// ErrBindCodeAlreadyUsed is returned when a bind code has already been consumed.
	ErrBindCodeAlreadyUsed = errors.New("bind code already used")
	// ErrBindCodeExpired is returned when a bind code has passed its expiry.
	ErrBindCodeExpired = errors.New("bind code expired")
)
