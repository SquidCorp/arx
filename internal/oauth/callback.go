package oauth

import (
	"net/http"
	"net/url"
)

// Callback handles GET /oauth/callback.
// It matches the pending flow by UUID, checks that the merchant webhook has fired
// (flow status == "connected"), and redirects to the LLM's redirect_uri with the
// authorization code and original state parameter.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	uuid := r.URL.Query().Get("uuid")
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "missing_uuid")
		return
	}

	// Try cache first, then database.
	flow, err := h.getFlow(r, uuid)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow_not_found")
		return
	}

	// Flow must be in "connected" state (merchant webhook has fired).
	if flow.Status != "connected" {
		switch flow.Status {
		case "pending":
			writeError(w, http.StatusAccepted, "flow_pending")
		case "expired":
			writeError(w, http.StatusGone, "flow_expired")
		case "consumed":
			writeError(w, http.StatusConflict, "flow_already_consumed")
		default:
			writeError(w, http.StatusBadRequest, "invalid_flow_status")
		}
		return
	}

	// Build redirect to the LLM's callback with code and state.
	redirectURI, err := url.Parse(flow.RedirectURI)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	query := redirectURI.Query()
	query.Set("code", flow.AuthCode)
	query.Set("state", flow.State)
	redirectURI.RawQuery = query.Encode()

	http.Redirect(w, r, redirectURI.String(), http.StatusFound)
}

// getFlow looks up the flow from cache, falling back to the database.
func (h *Handler) getFlow(r *http.Request, uuid string) (*PendingFlow, error) {
	// Try cache first.
	if h.oauthCache != nil {
		cached, err := h.oauthCache.Get(r.Context(), uuid)
		if err == nil && cached != nil {
			return &PendingFlow{
				UUID:                uuid,
				TenantID:            cached.TenantID,
				ClientID:            cached.ClientID,
				RedirectURI:         cached.RedirectURI,
				State:               cached.State,
				CodeChallenge:       cached.CodeChallenge,
				CodeChallengeMethod: cached.CodeChallengeMethod,
				AuthCode:            cached.AuthCode,
				SessionID:           cached.SessionID,
				Status:              cached.Status,
				ExpiresAt:           cached.ExpiresAt,
			}, nil
		}
	}

	// Fallback to database.
	return h.flowStore.GetPendingFlow(r.Context(), uuid)
}
