package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fambr/arx/internal/token"
)

func TestCallback_Success(t *testing.T) {
	flowStore := newMockFlowStore()
	flowStore.flows["flow-123"] = &PendingFlow{
		UUID:        "flow-123",
		TenantID:    "tenant-1",
		RedirectURI: "http://llm.example.com/callback",
		State:       "state-abc",
		AuthCode:    "auth-code-xyz",
		Status:      "connected",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}

	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(flowStore, nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?uuid=flow-123", nil)
	rec := httptest.NewRecorder()

	h.Callback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}

	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("missing Location header")
	}

	// Should redirect to LLM callback with code and state.
	if !containsSubstr(loc, "http://llm.example.com/callback") {
		t.Errorf("expected redirect to LLM callback, got %s", loc)
	}
	if !containsSubstr(loc, "code=auth-code-xyz") {
		t.Errorf("expected code param in redirect, got %s", loc)
	}
	if !containsSubstr(loc, "state=state-abc") {
		t.Errorf("expected state param in redirect, got %s", loc)
	}
}

func TestCallback_FlowPending(t *testing.T) {
	flowStore := newMockFlowStore()
	flowStore.flows["flow-pending"] = &PendingFlow{
		UUID:      "flow-pending",
		Status:    "pending",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(flowStore, nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?uuid=flow-pending", nil)
	rec := httptest.NewRecorder()

	h.Callback(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "flow_pending")
}

func TestCallback_FlowExpired(t *testing.T) {
	flowStore := newMockFlowStore()
	flowStore.flows["flow-expired"] = &PendingFlow{
		UUID:      "flow-expired",
		Status:    "expired",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}

	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(flowStore, nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?uuid=flow-expired", nil)
	rec := httptest.NewRecorder()

	h.Callback(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "flow_expired")
}

func TestCallback_MissingUUID(t *testing.T) {
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(newMockFlowStore(), nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback", nil)
	rec := httptest.NewRecorder()

	h.Callback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "missing_uuid")
}

func TestCallback_FlowNotFound(t *testing.T) {
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(newMockFlowStore(), nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?uuid=nonexistent", nil)
	rec := httptest.NewRecorder()

	h.Callback(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "flow_not_found")
}
