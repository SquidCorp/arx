package testapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/webhook"
	"github.com/go-chi/chi/v5"
)

// mockStore implements Store for testing.
type mockStore struct {
	mu       sync.Mutex
	sessions map[string]*webhook.SessionRecord
	nextID   int
}

func newMockStore() *mockStore {
	return &mockStore{sessions: make(map[string]*webhook.SessionRecord)}
}

func (m *mockStore) CreateSession(_ context.Context, rec *webhook.SessionRecord) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := "fixture-" + strings.Repeat("0", 5) + string(rune('0'+m.nextID))
	rec.ID = id
	m.sessions[id] = rec
	return id, nil
}

func (m *mockStore) UpdateSessionStatus(_ context.Context, id string, status session.Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return webhook.ErrSessionNotFound
	}
	s.Status = status
	return nil
}

func (m *mockStore) SetRefreshCount(_ context.Context, id string, count int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return webhook.ErrSessionNotFound
	}
	s.RefreshCount = count
	return nil
}

func (m *mockStore) getSession(id string) *webhook.SessionRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// --- CreateFixtureSession tests ---

func TestCreateFixtureSession_ActiveSession(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"tenantId":"t-1","status":"active","scopes":["cart.view"],"expiresIn":3600}`
	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["sessionId"] == "" {
		t.Error("expected non-empty sessionId")
	}

	sess := store.getSession(resp["sessionId"])
	if sess == nil {
		t.Fatal("session not found in store")
	}
	if sess.Status != session.StatusActive {
		t.Errorf("expected status active, got %s", sess.Status)
	}
	if sess.TenantID != "t-1" {
		t.Errorf("expected tenantId t-1, got %s", sess.TenantID)
	}
}

func TestCreateFixtureSession_SuspendedSession(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"tenantId":"t-1","status":"suspended","scopes":["cart.view"],"expiresIn":3600}`
	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	sess := store.getSession(resp["sessionId"])
	if sess.Status != session.StatusSuspended {
		t.Errorf("expected status suspended, got %s", sess.Status)
	}
}

func TestCreateFixtureSession_WithRefreshCount(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"tenantId":"t-1","status":"active","scopes":["cart.view"],"expiresIn":3600,"refreshCount":5}`
	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	sess := store.getSession(resp["sessionId"])
	if sess.RefreshCount != 5 {
		t.Errorf("expected refreshCount 5, got %d", sess.RefreshCount)
	}
}

func TestCreateFixtureSession_MissingTenantID(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"status":"active","scopes":["cart.view"],"expiresIn":3600}`
	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "missing_tenant_id")
}

func TestCreateFixtureSession_InvalidStatus(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"tenantId":"t-1","status":"bogus","scopes":["cart.view"],"expiresIn":3600}`
	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_status")
}

func TestCreateFixtureSession_InvalidExpiresIn(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"tenantId":"t-1","status":"active","scopes":["cart.view"],"expiresIn":0}`
	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_expires_in")
}

func TestCreateFixtureSession_InvalidJSON(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_payload")
}

func TestCreateFixtureSession_ExpiresAtIsSet(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"tenantId":"t-1","status":"active","scopes":["cart.view"],"expiresIn":3600}`
	req := httptest.NewRequest(http.MethodPost, "/internal/test/sessions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateFixtureSession(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	sess := store.getSession(resp["sessionId"])
	if time.Until(sess.ExpiresAt) < 59*time.Minute {
		t.Errorf("expected expiry ~1h from now, got %v", sess.ExpiresAt)
	}
}

// --- UpdateSession tests ---

func TestUpdateSession_SetStatus(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	store.sessions["sess-1"] = &webhook.SessionRecord{
		ID:     "sess-1",
		Status: session.StatusActive,
	}

	body := `{"status":"suspended"}`
	r := chi.NewRouter()
	r.Patch("/internal/test/sessions/{id}", h.UpdateSession)

	req := httptest.NewRequest(http.MethodPatch, "/internal/test/sessions/sess-1", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	sess := store.getSession("sess-1")
	if sess.Status != session.StatusSuspended {
		t.Errorf("expected suspended, got %s", sess.Status)
	}
}

func TestUpdateSession_SetRefreshCount(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	store.sessions["sess-2"] = &webhook.SessionRecord{
		ID:           "sess-2",
		Status:       session.StatusActive,
		RefreshCount: 0,
	}

	body := `{"refreshCount":5}`
	r := chi.NewRouter()
	r.Patch("/internal/test/sessions/{id}", h.UpdateSession)

	req := httptest.NewRequest(http.MethodPatch, "/internal/test/sessions/sess-2", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	sess := store.getSession("sess-2")
	if sess.RefreshCount != 5 {
		t.Errorf("expected refreshCount 5, got %d", sess.RefreshCount)
	}
}

func TestUpdateSession_SetBothStatusAndRefreshCount(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	store.sessions["sess-3"] = &webhook.SessionRecord{
		ID:           "sess-3",
		Status:       session.StatusActive,
		RefreshCount: 0,
	}

	body := `{"status":"expired","refreshCount":3}`
	r := chi.NewRouter()
	r.Patch("/internal/test/sessions/{id}", h.UpdateSession)

	req := httptest.NewRequest(http.MethodPatch, "/internal/test/sessions/sess-3", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	sess := store.getSession("sess-3")
	if sess.Status != session.StatusExpired {
		t.Errorf("expected expired, got %s", sess.Status)
	}
	if sess.RefreshCount != 3 {
		t.Errorf("expected refreshCount 3, got %d", sess.RefreshCount)
	}
}

func TestUpdateSession_SessionNotFound(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	body := `{"status":"suspended"}`
	r := chi.NewRouter()
	r.Patch("/internal/test/sessions/{id}", h.UpdateSession)

	req := httptest.NewRequest(http.MethodPatch, "/internal/test/sessions/nonexistent", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "session_not_found")
}

func TestUpdateSession_InvalidStatus(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	store.sessions["sess-4"] = &webhook.SessionRecord{
		ID:     "sess-4",
		Status: session.StatusActive,
	}

	body := `{"status":"bogus"}`
	r := chi.NewRouter()
	r.Patch("/internal/test/sessions/{id}", h.UpdateSession)

	req := httptest.NewRequest(http.MethodPatch, "/internal/test/sessions/sess-4", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_status")
}

func TestUpdateSession_InvalidJSON(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	r := chi.NewRouter()
	r.Patch("/internal/test/sessions/{id}", h.UpdateSession)

	req := httptest.NewRequest(http.MethodPatch, "/internal/test/sessions/sess-1", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_payload")
}

func TestUpdateSession_EmptyBody(t *testing.T) {
	store := newMockStore()
	h := NewHandler(store)

	store.sessions["sess-5"] = &webhook.SessionRecord{
		ID:     "sess-5",
		Status: session.StatusActive,
	}

	body := `{}`
	r := chi.NewRouter()
	r.Patch("/internal/test/sessions/{id}", h.UpdateSession)

	req := httptest.NewRequest(http.MethodPatch, "/internal/test/sessions/sess-5", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "no_fields_to_update")
}

// assertErrorCode checks the JSON error response body.
func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, expectedCode string) {
	t.Helper()
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if resp["error"] != expectedCode {
		t.Errorf("expected error code %q, got %q", expectedCode, resp["error"])
	}
}
