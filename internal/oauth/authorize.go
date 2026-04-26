package oauth

import (
	"net/http"
	"net/url"
	"time"

	"github.com/fambr/arx/internal/cache"
)

// Authorize handles GET /oauth/authorize.
// It validates the request parameters, creates a pending OAuth flow,
// and redirects the user to the merchant's login URL.
func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	// Validate required parameters.
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "missing_client_id")
		return
	}
	if redirectURI == "" {
		writeError(w, http.StatusBadRequest, "missing_redirect_uri")
		return
	}
	if state == "" {
		writeError(w, http.StatusBadRequest, "missing_state")
		return
	}
	if codeChallenge == "" {
		writeError(w, http.StatusBadRequest, "missing_code_challenge")
		return
	}
	if codeChallengeMethod != "S256" {
		writeError(w, http.StatusBadRequest, "invalid_code_challenge_method")
		return
	}

	// Look up tenant by client_id.
	tenant, err := h.flowStore.GetTenantOAuth(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_client_id")
		return
	}

	// Validate redirect_uri against tenant's allowed list.
	if !isAllowedRedirectURI(redirectURI, tenant.RedirectURIs) {
		writeError(w, http.StatusBadRequest, "invalid_redirect_uri")
		return
	}

	// Generate a flow UUID.
	uuid, err := generateUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Generate an authorization code (stored for later exchange).
	authCode, err := generateAuthCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	flow := &PendingFlow{
		UUID:                uuid,
		TenantID:            tenant.ID,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		State:               state,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		AuthCode:            authCode,
		Status:              "pending",
		ExpiresAt:           time.Now().Add(flowTTL),
	}

	// Persist to database.
	if err := h.flowStore.CreatePendingFlow(r.Context(), flow); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Cache the flow (best-effort).
	if h.oauthCache != nil {
		_ = h.oauthCache.Set(r.Context(), uuid, &cache.OAuthFlowData{
			TenantID:            flow.TenantID,
			ClientID:            flow.ClientID,
			RedirectURI:         flow.RedirectURI,
			State:               flow.State,
			CodeChallenge:       flow.CodeChallenge,
			CodeChallengeMethod: flow.CodeChallengeMethod,
			AuthCode:            flow.AuthCode,
			Status:              flow.Status,
			ExpiresAt:           flow.ExpiresAt,
		})
	}

	// Redirect to merchant login URL.
	loginURL, err := url.Parse(tenant.MerchantLoginURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	query := loginURL.Query()
	query.Set("agent_session_uuid", uuid)
	loginURL.RawQuery = query.Encode()

	http.Redirect(w, r, loginURL.String(), http.StatusFound)
}

// isAllowedRedirectURI checks if the given URI is in the tenant's allowed list.
func isAllowedRedirectURI(uri string, allowed []string) bool {
	for _, a := range allowed {
		if a == uri {
			return true
		}
	}
	return false
}
