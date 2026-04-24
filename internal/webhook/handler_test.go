package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/session"
)

// mockSessionStore is an in-memory SessionStore for testing.
type mockSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*SessionRecord
	tenants  map[string]*TenantRecord
	nextID   int
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{
		sessions: make(map[string]*SessionRecord),
		tenants:  make(map[string]*TenantRecord),
	}
}

func (m *mockSessionStore) CreateSession(_ context.Context, rec *SessionRecord) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := "sess-" + string(rune('0'+m.nextID))
	rec.ID = id
	m.sessions[id] = rec
	return id, nil
}

func (m *mockSessionStore) GetSession(_ context.Context, id string) (*SessionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

func (m *mockSessionStore) UpdateSessionStatus(_ context.Context, id string, status session.Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	s.Status = status
	return nil
}

func (m *mockSessionStore) RefreshSession(_ context.Context, id string, expiresAt time.Time, scopes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	s.ExpiresAt = expiresAt
	s.Scopes = scopes
	s.RefreshCount++
	return nil
}

func (m *mockSessionStore) GetTenant(_ context.Context, tenantID string) (*TenantRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return t, nil
}

func (m *mockSessionStore) addTenant(t *TenantRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tenants[t.ID] = t
}

// mockFlowLinker is a mock OAuthFlowLinker for testing.
type mockFlowLinker struct {
	mu         sync.Mutex
	flows      map[string]string // uuid -> sessionID
	knownUUIDs map[string]bool
}

func newMockFlowLinker(knownUUIDs ...string) *mockFlowLinker {
	known := make(map[string]bool)
	for _, u := range knownUUIDs {
		known[u] = true
	}
	return &mockFlowLinker{
		flows:      make(map[string]string),
		knownUUIDs: known,
	}
}

func (m *mockFlowLinker) LinkSessionToFlow(_ context.Context, uuid, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.knownUUIDs[uuid] {
		return errors.New("unknown_flow")
	}
	m.flows[uuid] = sessionID
	return nil
}

// handlerTestSetup creates a Handler with mock dependencies and a real Redis session cache.
type handlerTestSetup struct {
	handler      *Handler
	store        *mockSessionStore
	flowLinker   *mockFlowLinker
	sessionCache *cache.SessionCache
	mini         *miniredis.Miniredis
}

func newHandlerTestSetup(t *testing.T, knownUUIDs ...string) *handlerTestSetup {
	t.Helper()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mini.Close)

	cacheClient, err := cache.Connect(context.Background(), "redis://"+mini.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cacheClient.Close() })

	sessionCache := cache.NewSessionCache(cacheClient)
	store := newMockSessionStore()
	fl := newMockFlowLinker(knownUUIDs...)

	store.addTenant(&TenantRecord{
		ID:               "tenant-123",
		SessionMaxExpiry: 1 * time.Hour,
		MaxRefreshes:     5,
	})

	h := NewHandler(store, sessionCache, fl)

	return &handlerTestSetup{
		handler:      h,
		store:        store,
		flowLinker:   fl,
		sessionCache: sessionCache,
		mini:         mini,
	}
}

func makeRequest(t *testing.T, tenantID string, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest("POST", "/webhook/test", nil)
	ctx := context.WithValue(req.Context(), tenantIDCtxKey{}, tenantID)
	ctx = context.WithValue(ctx, bodyCtxKey{}, []byte(body))
	return req.WithContext(ctx)
}

// --- SessionConnected tests ---

func TestSessionConnected_Flow2_ReturnsBindCode(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := `{"sessionId":"msid-1","scopes":["cart.view"],"expiresIn":3600}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionConnected(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["bindCode"] == "" {
		t.Error("expected non-empty bindCode")
	}
	if resp["sessionId"] == "" {
		t.Error("expected non-empty sessionId")
	}
}

func TestSessionConnected_Flow1_ReturnsPending(t *testing.T) {
	ts := newHandlerTestSetup(t, "flow-uuid-1")

	body := `{"sessionId":"msid-2","scopes":["cart.view"],"expiresIn":3600,"uuid":"flow-uuid-1"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionConnected(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "pending" {
		t.Errorf("expected status=pending, got %q", resp["status"])
	}
}

func TestSessionConnected_Flow1_UnknownUUID(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := `{"sessionId":"msid-3","scopes":["cart.view"],"expiresIn":3600,"uuid":"not-real"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionConnected(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "unknown_flow")
}

func TestSessionConnected_ExpiresInCappedByTenant(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Request 2 hours, but tenant max is 1 hour.
	body := `{"sessionId":"msid-cap","scopes":["cart.view"],"expiresIn":7200}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionConnected(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the created session has expiry capped at ~1 hour.
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	sess, err := ts.store.GetSession(context.Background(), resp["sessionId"])
	if err != nil {
		t.Fatal(err)
	}
	expiryDuration := time.Until(sess.ExpiresAt)
	if expiryDuration > 61*time.Minute {
		t.Errorf("expected expiry <= 1 hour, got %v", expiryDuration)
	}
}

func TestSessionConnected_MissingSessionID(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := `{"scopes":["cart.view"],"expiresIn":3600}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionConnected(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "missing_session_id")
}

func TestSessionConnected_MissingScopes(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := `{"sessionId":"msid-4","expiresIn":3600}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionConnected(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "missing_scopes")
}

func TestSessionConnected_InvalidExpiresIn(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := `{"sessionId":"msid-5","scopes":["cart.view"],"expiresIn":0}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionConnected(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_expires_in")
}

// --- SessionRefresh tests ---

func TestSessionRefresh_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Pre-create a session.
	ts.store.sessions["sess-r1"] = &SessionRecord{
		ID:           "sess-r1",
		TenantID:     "tenant-123",
		MerchantSID:  "msid-r1",
		Status:       session.StatusActive,
		Scopes:       []string{"cart.view"},
		RefreshCount: 0,
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	}

	body := `{"sessionId":"sess-r1","expiresIn":3600}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRefresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "refreshed" {
		t.Errorf("expected status=refreshed, got %q", resp["status"])
	}
}

func TestSessionRefresh_WithUpdatedScopes(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-r2"] = &SessionRecord{
		ID:           "sess-r2",
		TenantID:     "tenant-123",
		MerchantSID:  "msid-r2",
		Status:       session.StatusActive,
		Scopes:       []string{"cart.view"},
		RefreshCount: 0,
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	}

	body := `{"sessionId":"sess-r2","expiresIn":3600,"scopes":["cart.view","cart.add"]}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRefresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	sess, _ := ts.store.GetSession(context.Background(), "sess-r2")
	if len(sess.Scopes) != 2 || sess.Scopes[1] != "cart.add" {
		t.Errorf("expected updated scopes, got %v", sess.Scopes)
	}
}

func TestSessionRefresh_MaxRefreshesExceeded(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-r3"] = &SessionRecord{
		ID:           "sess-r3",
		TenantID:     "tenant-123",
		MerchantSID:  "msid-r3",
		Status:       session.StatusActive,
		Scopes:       []string{"cart.view"},
		RefreshCount: 5, // Already at max.
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	}

	body := `{"sessionId":"sess-r3","expiresIn":3600}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRefresh(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "max_refreshes_exceeded")
}

func TestSessionRefresh_NonActiveSession(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-r4"] = &SessionRecord{
		ID:           "sess-r4",
		TenantID:     "tenant-123",
		MerchantSID:  "msid-r4",
		Status:       session.StatusRevoked,
		Scopes:       []string{"cart.view"},
		RefreshCount: 0,
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	}

	body := `{"sessionId":"sess-r4","expiresIn":3600}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRefresh(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "session_not_active")
}

func TestSessionRefresh_SessionNotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := `{"sessionId":"nonexistent","expiresIn":3600}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRefresh(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "session_not_found")
}

// --- SessionRevoked tests ---

func TestSessionRevoked_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-rev1"] = &SessionRecord{
		ID:       "sess-rev1",
		TenantID: "tenant-123",
		Status:   session.StatusActive,
	}

	body := `{"sessionId":"sess-rev1"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRevoked(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "revoked" {
		t.Errorf("expected status=revoked, got %q", resp["status"])
	}

	// Verify status updated.
	sess, _ := ts.store.GetSession(context.Background(), "sess-rev1")
	if sess.Status != session.StatusRevoked {
		t.Errorf("expected session status revoked, got %s", sess.Status)
	}
}

func TestSessionRevoked_Idempotent(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-rev2"] = &SessionRecord{
		ID:       "sess-rev2",
		TenantID: "tenant-123",
		Status:   session.StatusRevoked, // Already revoked.
	}

	body := `{"sessionId":"sess-rev2"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRevoked(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "revoked" {
		t.Errorf("expected status=revoked, got %q", resp["status"])
	}
}

func TestSessionRevoked_FromSuspended(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-rev3"] = &SessionRecord{
		ID:       "sess-rev3",
		TenantID: "tenant-123",
		Status:   session.StatusSuspended,
	}

	body := `{"sessionId":"sess-rev3"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRevoked(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	sess, _ := ts.store.GetSession(context.Background(), "sess-rev3")
	if sess.Status != session.StatusRevoked {
		t.Errorf("expected status revoked, got %s", sess.Status)
	}
}

func TestSessionRevoked_FromExpired(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-rev4"] = &SessionRecord{
		ID:       "sess-rev4",
		TenantID: "tenant-123",
		Status:   session.StatusExpired,
	}

	body := `{"sessionId":"sess-rev4"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionRevoked(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_state_transition")
}

// --- SessionResumed tests ---

func TestSessionResumed_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-res1"] = &SessionRecord{
		ID:        "sess-res1",
		TenantID:  "tenant-123",
		Status:    session.StatusSuspended,
		Scopes:    []string{"cart.view"},
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	body := `{"sessionId":"sess-res1"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionResumed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "active" {
		t.Errorf("expected status=active, got %q", resp["status"])
	}

	sess, _ := ts.store.GetSession(context.Background(), "sess-res1")
	if sess.Status != session.StatusActive {
		t.Errorf("expected session status active, got %s", sess.Status)
	}
}

func TestSessionResumed_NotSuspended(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-res2"] = &SessionRecord{
		ID:       "sess-res2",
		TenantID: "tenant-123",
		Status:   session.StatusActive,
	}

	body := `{"sessionId":"sess-res2"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionResumed(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "session_not_suspended")
}

func TestSessionResumed_SessionNotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := `{"sessionId":"nonexistent"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionResumed(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "session_not_found")
}

func TestSessionResumed_RevokedCannotResume(t *testing.T) {
	ts := newHandlerTestSetup(t)

	ts.store.sessions["sess-res3"] = &SessionRecord{
		ID:       "sess-res3",
		TenantID: "tenant-123",
		Status:   session.StatusRevoked,
	}

	body := `{"sessionId":"sess-res3"}`
	req := makeRequest(t, "tenant-123", body)
	rec := httptest.NewRecorder()

	ts.handler.SessionResumed(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "session_not_suspended")
}
