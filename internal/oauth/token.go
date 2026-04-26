package oauth

import (
	"net/http"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/token"
)

// Token handles POST /oauth/token.
// It supports grant_type=authorization_code and grant_type=refresh_token.
func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.tokenAuthorizationCode(w, r)
	case "refresh_token":
		h.tokenRefreshToken(w, r)
	default:
		writeError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

// tokenAuthorizationCode handles authorization code exchange with PKCE and DPoP.
func (h *Handler) tokenAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")

	if code == "" {
		writeError(w, http.StatusBadRequest, "missing_code")
		return
	}
	if codeVerifier == "" {
		writeError(w, http.StatusBadRequest, "missing_code_verifier")
		return
	}

	// Require DPoP proof header.
	dpopProof := r.Header.Get("DPoP")
	if dpopProof == "" {
		writeError(w, http.StatusBadRequest, "missing_dpop_proof")
		return
	}

	// Look up and consume the pending flow by auth code.
	flow, err := h.flowStore.ConsumePendingFlow(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Verify PKCE: SHA256(code_verifier) must match code_challenge.
	if !verifyPKCE(codeVerifier, flow.CodeChallenge) {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Validate DPoP proof.
	thumbprint, err := token.ValidateDPoPProof(dpopProof, "", r.Method, token.RequestURL(r), h.nonceCheck(r.Context()))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_dpop_proof")
		return
	}

	// Load session to get scopes and expiry.
	sess, err := h.sessionStore.GetSession(r.Context(), flow.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if sess.Status != session.StatusActive {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Look up tenant keys for signing.
	keys, err := h.keyLookup(r.Context(), flow.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Issue access token.
	accessToken, err := h.issuer.IssueAccessToken(r.Context(), keys, sess.ID, flow.TenantID, thumbprint, sess.Scopes, sess.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Issue refresh token.
	refreshToken, err := h.issuer.IssueRefreshToken(r.Context(), keys, sess.ID, flow.TenantID, sess.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Cache the session for the hot path.
	remaining := time.Until(sess.ExpiresAt)
	if remaining > 0 && h.sessionCache != nil {
		_ = h.sessionCache.Set(r.Context(), sess.ID, &cache.SessionData{
			TenantID:  sess.TenantID,
			Status:    string(sess.Status),
			Scopes:    sess.Scopes,
			ExpiresAt: sess.ExpiresAt,
		}, remaining)
	}

	expiresIn := int64(h.issuer.Config().AccessTokenTTL.Seconds())
	if sessionRemaining := int64(remaining.Seconds()); sessionRemaining > 0 && sessionRemaining < expiresIn {
		expiresIn = sessionRemaining
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "DPoP",
		"expires_in":    expiresIn,
	})
}

// tokenRefreshToken handles refresh token exchange.
func (h *Handler) tokenRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshTokenStr := r.FormValue("refresh_token")
	if refreshTokenStr == "" {
		writeError(w, http.StatusBadRequest, "missing_refresh_token")
		return
	}

	// Parse the refresh token to extract claims (unverified first to get tenant).
	unverifiedClaims, err := token.ParseRefreshTokenUnverified(refreshTokenStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	tenantID := unverifiedClaims.Tenant
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Look up tenant keys to verify token signature.
	keys, err := h.keyLookup(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Verify refresh token signature.
	claims, err := token.ParseRefreshToken(refreshTokenStr, keys.ArxSigningPublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Look up the session.
	sess, err := h.sessionStore.GetSession(r.Context(), claims.SessionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Check session state — must be active.
	if sess.Status != session.StatusActive {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Check session expiry.
	if sess.ExpiresAt.Before(time.Now()) {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Check max refreshes.
	policy, err := h.sessionStore.GetTenantPolicy(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if sess.RefreshCount >= policy.MaxRefreshes {
		writeError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Refresh the session (increment count, extend expiry).
	newExpiry := time.Now().Add(policy.SessionMaxExpiry)
	if err := h.sessionStore.RefreshSession(r.Context(), sess.ID, newExpiry, sess.Scopes); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// DPoP proof is optional on refresh but if present, validate it.
	thumbprint := ""
	dpopProof := r.Header.Get("DPoP")
	if dpopProof != "" {
		tp, err := token.ValidateDPoPProof(dpopProof, "", r.Method, token.RequestURL(r), h.nonceCheck(r.Context()))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_dpop_proof")
			return
		}
		thumbprint = tp
	}

	// Issue new access token.
	accessToken, err := h.issuer.IssueAccessToken(r.Context(), keys, sess.ID, tenantID, thumbprint, sess.Scopes, newExpiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Issue new refresh token.
	refreshToken, err := h.issuer.IssueRefreshToken(r.Context(), keys, sess.ID, tenantID, newExpiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Update session cache.
	remaining := time.Until(newExpiry)
	if remaining > 0 && h.sessionCache != nil {
		_ = h.sessionCache.Set(r.Context(), sess.ID, &cache.SessionData{
			TenantID:  sess.TenantID,
			Status:    string(sess.Status),
			Scopes:    sess.Scopes,
			ExpiresAt: newExpiry,
		}, remaining)
	}

	expiresIn := int64(h.issuer.Config().AccessTokenTTL.Seconds())
	if sessionRemaining := int64(remaining.Seconds()); sessionRemaining > 0 && sessionRemaining < expiresIn {
		expiresIn = sessionRemaining
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "DPoP",
		"expires_in":    expiresIn,
	})
}
