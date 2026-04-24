package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/session"
)

const (
	bindCodeLength = 32
	bindCodeTTL    = 60 * time.Second
)

// SessionRecord represents a session row in the database.
type SessionRecord struct {
	ID              string
	TenantID        string
	MerchantSID     string
	UserID          string
	IsAnonymous     bool
	Status          session.Status
	Scopes          []string
	ClaimsEnc       []byte
	BindCode        string
	BindCodeExpires *time.Time
	RefreshCount    int
	ExpiresAt       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TenantRecord holds the session policy fields needed by webhook handlers.
type TenantRecord struct {
	ID               string
	SessionMaxExpiry time.Duration
	MaxRefreshes     int
}

// SessionStore abstracts database operations for sessions.
type SessionStore interface {
	// CreateSession inserts a new session and returns its ID.
	CreateSession(ctx context.Context, rec *SessionRecord) (string, error)

	// GetSession retrieves a session by ID.
	GetSession(ctx context.Context, id string) (*SessionRecord, error)

	// UpdateSessionStatus updates a session's status.
	UpdateSessionStatus(ctx context.Context, id string, status session.Status) error

	// RefreshSession updates expiry, scopes, and increments refresh_count.
	RefreshSession(ctx context.Context, id string, expiresAt time.Time, scopes []string) error

	// GetTenant retrieves a tenant's session policy.
	GetTenant(ctx context.Context, tenantID string) (*TenantRecord, error)
}

// OAuthFlowLinker handles linking session creation to pending OAuth flows (Flow 1).
type OAuthFlowLinker interface {
	// LinkSessionToFlow links the session to a pending OAuth flow and generates an auth code.
	// Returns an error if the UUID does not match any pending flow.
	LinkSessionToFlow(ctx context.Context, uuid, sessionID string) error
}

// Handler handles webhook HTTP requests for session lifecycle events.
type Handler struct {
	sessions     SessionStore
	sessionCache *cache.SessionCache
	flowLinker   OAuthFlowLinker
}

// NewHandler creates a webhook Handler with the given dependencies.
func NewHandler(sessions SessionStore, sessionCache *cache.SessionCache, flowLinker OAuthFlowLinker) *Handler {
	return &Handler{
		sessions:     sessions,
		sessionCache: sessionCache,
		flowLinker:   flowLinker,
	}
}

// sessionConnectedRequest is the JSON payload for POST /webhook/session-connected.
type sessionConnectedRequest struct {
	SessionID   string   `json:"sessionId"`
	IsAnonymous bool     `json:"isAnonymous"`
	Scopes      []string `json:"scopes"`
	ExpiresIn   int      `json:"expiresIn"`
	UUID        string   `json:"uuid,omitempty"`
	UserID      string   `json:"userId,omitempty"`
	Claims      any      `json:"claims,omitempty"`
}

// SessionConnected handles POST /webhook/session-connected.
// For Flow 2 (no uuid): creates a session and returns a bind_code.
// For Flow 1 (with uuid): creates a session, links to OAuth flow, returns status:pending.
func (h *Handler) SessionConnected(w http.ResponseWriter, r *http.Request) {
	body, ok := BodyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusBadRequest, "missing_body")
		return
	}

	tenantID, ok := TenantIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusBadRequest, "missing_tenant_id")
		return
	}

	var req sessionConnectedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "missing_session_id")
		return
	}
	if len(req.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, "missing_scopes")
		return
	}
	if req.ExpiresIn <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_expires_in")
		return
	}

	// Look up tenant to cap expiresIn.
	tenant, err := h.sessions.GetTenant(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	expiry := time.Duration(req.ExpiresIn) * time.Second
	if expiry > tenant.SessionMaxExpiry {
		expiry = tenant.SessionMaxExpiry
	}
	expiresAt := time.Now().Add(expiry)

	rec := &SessionRecord{
		TenantID:    tenantID,
		MerchantSID: req.SessionID,
		UserID:      req.UserID,
		IsAnonymous: req.IsAnonymous,
		Status:      session.StatusActive,
		Scopes:      req.Scopes,
		ExpiresAt:   expiresAt,
	}

	// Flow 2: Generate bind code.
	if req.UUID == "" {
		code, genErr := generateBindCode()
		if genErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		bindExpires := time.Now().Add(bindCodeTTL)
		rec.BindCode = code
		rec.BindCodeExpires = &bindExpires

		id, createErr := h.sessions.CreateSession(r.Context(), rec)
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		// Write-through to cache.
		_ = h.sessionCache.Set(r.Context(), id, &cache.SessionData{
			TenantID:    tenantID,
			MerchantSID: req.SessionID,
			UserID:      req.UserID,
			Status:      string(session.StatusActive),
			Scopes:      req.Scopes,
			ExpiresAt:   expiresAt,
		}, expiry)

		writeJSON(w, http.StatusCreated, map[string]string{
			"sessionId": id,
			"bindCode":  code,
		})
		return
	}

	// Flow 1: Link to OAuth pending flow.
	id, createErr := h.sessions.CreateSession(r.Context(), rec)
	if createErr != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if err := h.flowLinker.LinkSessionToFlow(r.Context(), req.UUID, id); err != nil {
		writeError(w, http.StatusNotFound, "unknown_flow")
		return
	}

	// Write-through to cache.
	_ = h.sessionCache.Set(r.Context(), id, &cache.SessionData{
		TenantID:    tenantID,
		MerchantSID: req.SessionID,
		UserID:      req.UserID,
		Status:      string(session.StatusActive),
		Scopes:      req.Scopes,
		ExpiresAt:   expiresAt,
	}, expiry)

	writeJSON(w, http.StatusCreated, map[string]string{
		"status": "pending",
	})
}

// sessionRefreshRequest is the JSON payload for POST /webhook/session-refresh.
type sessionRefreshRequest struct {
	SessionID string   `json:"sessionId"`
	ExpiresIn int      `json:"expiresIn"`
	Scopes    []string `json:"scopes,omitempty"`
}

// SessionRefresh handles POST /webhook/session-refresh.
// Updates the session expiry, optionally replaces scopes, and increments refresh_count.
func (h *Handler) SessionRefresh(w http.ResponseWriter, r *http.Request) {
	body, ok := BodyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusBadRequest, "missing_body")
		return
	}

	tenantID, ok := TenantIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusBadRequest, "missing_tenant_id")
		return
	}

	var req sessionRefreshRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "missing_session_id")
		return
	}
	if req.ExpiresIn <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_expires_in")
		return
	}

	sess, err := h.sessions.GetSession(r.Context(), req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session_not_found")
		return
	}

	if sess.Status != session.StatusActive {
		writeError(w, http.StatusConflict, "session_not_active")
		return
	}

	// Look up tenant to check max_refreshes.
	tenant, err := h.sessions.GetTenant(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if sess.RefreshCount >= tenant.MaxRefreshes {
		writeError(w, http.StatusForbidden, "max_refreshes_exceeded")
		return
	}

	// Cap expiry at tenant max.
	expiry := time.Duration(req.ExpiresIn) * time.Second
	if expiry > tenant.SessionMaxExpiry {
		expiry = tenant.SessionMaxExpiry
	}
	expiresAt := time.Now().Add(expiry)

	scopes := sess.Scopes
	if len(req.Scopes) > 0 {
		scopes = req.Scopes
	}

	if err := h.sessions.RefreshSession(r.Context(), req.SessionID, expiresAt, scopes); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Write-through cache update.
	_ = h.sessionCache.Set(r.Context(), req.SessionID, &cache.SessionData{
		TenantID:    tenantID,
		MerchantSID: sess.MerchantSID,
		UserID:      sess.UserID,
		Status:      string(session.StatusActive),
		Scopes:      scopes,
		ExpiresAt:   expiresAt,
	}, expiry)

	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "refreshed",
		"sessionId": req.SessionID,
	})
}

// sessionRevokedRequest is the JSON payload for POST /webhook/session-revoked.
type sessionRevokedRequest struct {
	SessionID string `json:"sessionId"`
}

// SessionRevoked handles POST /webhook/session-revoked.
// Marks the session as REVOKED and invalidates the Redis cache.
// Idempotent — re-revoking an already-revoked session returns 200.
func (h *Handler) SessionRevoked(w http.ResponseWriter, r *http.Request) {
	body, ok := BodyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusBadRequest, "missing_body")
		return
	}

	var req sessionRevokedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "missing_session_id")
		return
	}

	sess, err := h.sessions.GetSession(r.Context(), req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session_not_found")
		return
	}

	// Idempotent — already revoked.
	if sess.Status == session.StatusRevoked {
		writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
		return
	}

	// Validate state transition.
	if err := session.ValidateTransition(sess.Status, session.StatusRevoked); err != nil {
		writeError(w, http.StatusConflict, "invalid_state_transition")
		return
	}

	if err := h.sessions.UpdateSessionStatus(r.Context(), req.SessionID, session.StatusRevoked); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Invalidate cache.
	_ = h.sessionCache.Delete(r.Context(), req.SessionID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// sessionResumedRequest is the JSON payload for POST /webhook/session-resumed.
type sessionResumedRequest struct {
	SessionID string `json:"sessionId"`
}

// SessionResumed handles POST /webhook/session-resumed.
// Marks a SUSPENDED session as ACTIVE. Rejects if the session is not suspended.
func (h *Handler) SessionResumed(w http.ResponseWriter, r *http.Request) {
	body, ok := BodyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusBadRequest, "missing_body")
		return
	}

	var req sessionResumedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "missing_session_id")
		return
	}

	sess, err := h.sessions.GetSession(r.Context(), req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session_not_found")
		return
	}

	if sess.Status != session.StatusSuspended {
		writeError(w, http.StatusConflict, "session_not_suspended")
		return
	}

	if err := h.sessions.UpdateSessionStatus(r.Context(), req.SessionID, session.StatusActive); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Update cache with active status.
	remaining := time.Until(sess.ExpiresAt)
	if remaining > 0 {
		_ = h.sessionCache.Set(r.Context(), req.SessionID, &cache.SessionData{
			TenantID:    sess.TenantID,
			MerchantSID: sess.MerchantSID,
			UserID:      sess.UserID,
			Status:      string(session.StatusActive),
			Scopes:      sess.Scopes,
			ExpiresAt:   sess.ExpiresAt,
		}, remaining)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// generateBindCode creates a cryptographically random hex-encoded bind code.
func generateBindCode() (string, error) {
	b := make([]byte, bindCodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ErrSessionNotFound is returned when a session lookup finds no matching row.
var ErrSessionNotFound = errors.New("session not found")
