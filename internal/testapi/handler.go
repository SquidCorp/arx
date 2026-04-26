// Package testapi provides test-only HTTP handlers for creating and
// manipulating fixture sessions during e2e testing. These endpoints are
// registered only when the application is started with APP_TEST_MODE=true.
package testapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/webhook"
	"github.com/go-chi/chi/v5"
)

// Store defines the storage operations required by the test API handlers.
type Store interface {
	CreateSession(ctx context.Context, rec *webhook.SessionRecord) (string, error)
	UpdateSessionStatus(ctx context.Context, id string, status session.Status) error
	SetRefreshCount(ctx context.Context, id string, count int) error
}

// Handler holds dependencies for the test API endpoints.
type Handler struct {
	store Store
}

// NewHandler creates a Handler backed by the given store.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// createRequest is the JSON body for POST /internal/test/sessions.
type createRequest struct {
	TenantID     string   `json:"tenantId"`
	Status       string   `json:"status"`
	Scopes       []string `json:"scopes"`
	ExpiresIn    int      `json:"expiresIn"`
	RefreshCount int      `json:"refreshCount"`
}

// updateRequest is the JSON body for PATCH /internal/test/sessions/{id}.
type updateRequest struct {
	Status       string `json:"status"`
	RefreshCount *int   `json:"refreshCount"`
}

// CreateFixtureSession creates a session in the requested state.
func (h *Handler) CreateFixtureSession(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}

	if req.TenantID == "" {
		writeError(w, http.StatusBadRequest, "missing_tenant_id")
		return
	}

	status, err := session.ParseStatus(req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_status")
		return
	}

	if req.ExpiresIn <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_expires_in")
		return
	}

	rec := &webhook.SessionRecord{
		TenantID:  req.TenantID,
		Status:    status,
		Scopes:    req.Scopes,
		ExpiresAt: time.Now().Add(time.Duration(req.ExpiresIn) * time.Second),
	}

	id, err := h.store.CreateSession(r.Context(), rec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if req.RefreshCount > 0 {
		if err := h.store.SetRefreshCount(r.Context(), id, req.RefreshCount); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]string{"sessionId": id})
}

// UpdateSession updates an existing session's status and/or refresh count.
func (h *Handler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}

	hasUpdate := false

	if req.Status != "" {
		if _, err := session.ParseStatus(req.Status); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_status")
			return
		}
		hasUpdate = true
	}

	if req.RefreshCount != nil {
		hasUpdate = true
	}

	if !hasUpdate {
		writeError(w, http.StatusBadRequest, "no_fields_to_update")
		return
	}

	if req.Status != "" {
		if err := h.store.UpdateSessionStatus(r.Context(), id, session.Status(req.Status)); err != nil {
			if errors.Is(err, webhook.ErrSessionNotFound) {
				writeError(w, http.StatusNotFound, "session_not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}

	if req.RefreshCount != nil {
		if err := h.store.SetRefreshCount(r.Context(), id, *req.RefreshCount); err != nil {
			if errors.Is(err, webhook.ErrSessionNotFound) {
				writeError(w, http.StatusNotFound, "session_not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
