package token

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/golang-jwt/jwt/v5"
)

// mockBindSessionStore is an in-memory BindSessionStore for testing.
type mockBindSessionStore struct {
	sessions map[string]*BindSession // key: bindCode
}

func newMockBindSessionStore() *mockBindSessionStore {
	return &mockBindSessionStore{sessions: make(map[string]*BindSession)}
}

func (m *mockBindSessionStore) GetAndConsumeBindCode(_ context.Context, bindCode string) (*BindSession, error) {
	s, ok := m.sessions[bindCode]
	if !ok {
		return nil, ErrBindCodeNotFound
	}
	if s.BindCodeExpires == nil {
		return nil, ErrBindCodeAlreadyUsed
	}
	if s.BindCodeExpires.Before(NowFunc()) {
		return nil, ErrBindCodeExpired
	}
	s.BindCodeExpires = nil
	return s, nil
}

func (m *mockBindSessionStore) addSession(bindCode string, s *BindSession) {
	m.sessions[bindCode] = s
}

// bindTestSetup creates a BindHandler with mock dependencies and a real Redis session cache.
type bindTestSetup struct {
	handler      *BindHandler
	store        *mockBindSessionStore
	sessionCache *cache.SessionCache
	mini         *miniredis.Miniredis
	km           crypto.KeyManager
	keys         crypto.TenantKeys
	issuer       *Issuer
}

func newBindTestSetup(t *testing.T) *bindTestSetup {
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
	store := newMockBindSessionStore()

	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	nonces := cache.NewNonceStore(cacheClient)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return keys, nil
	}

	h := NewBindHandler(store, sessionCache, keyLookup, issuer, nonces)

	return &bindTestSetup{
		handler:      h,
		store:        store,
		sessionCache: sessionCache,
		mini:         mini,
		km:           km,
		keys:         keys,
		issuer:       issuer,
	}
}

func makeBindRequest(t *testing.T, bindCode, dpopProof string) *http.Request {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"bind_code":  bindCode,
		"dpop_proof": dpopProof,
	})
	return httptest.NewRequest("POST", "http://localhost/token/bind", bytes.NewReader(body))
}

func TestBindHandler_Success(t *testing.T) {
	ts := newBindTestSetup(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	bindCode := "bind-123"
	expires := time.Now().Add(time.Hour)
	ts.store.addSession(bindCode, &BindSession{
		ID:              "sess-1",
		TenantID:        "tenant-1",
		Status:          session.StatusActive,
		Scopes:          []string{"cart.view"},
		BindCodeExpires: &expires,
		ExpiresAt:       expires,
	})

	dpopClaims := DPoPClaims{
		HTM: "POST",
		HTU: "http://localhost/token/bind",
	}
	dpopClaims.ID = "jti-bind-1"
	dpopClaims.IssuedAt = jwt.NewNumericDate(time.Now())

	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	req := makeBindRequest(t, bindCode, dpopProof)
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp bindResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.TokenType != "DPoP" {
		t.Errorf("expected token_type DPoP, got %q", resp.TokenType)
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expected positive expires_in, got %d", resp.ExpiresIn)
	}

	// Verify bind code was consumed.
	_, err = ts.store.GetAndConsumeBindCode(context.Background(), bindCode)
	if !errors.Is(err, ErrBindCodeAlreadyUsed) {
		t.Errorf("expected bind code to be consumed, got err=%v", err)
	}
}

func TestBindHandler_MissingBindCode(t *testing.T) {
	ts := newBindTestSetup(t)

	body, _ := json.Marshal(map[string]string{
		"dpop_proof": "some-proof",
	})
	req := httptest.NewRequest("POST", "/token/bind", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestBindHandler_MissingDPoPProof(t *testing.T) {
	ts := newBindTestSetup(t)

	body, _ := json.Marshal(map[string]string{
		"bind_code": "bind-123",
	})
	req := httptest.NewRequest("POST", "/token/bind", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestBindHandler_InvalidBindCode(t *testing.T) {
	ts := newBindTestSetup(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	dpopClaims := DPoPClaims{
		HTM: "POST",
		HTU: "http://localhost/token/bind",
	}
	dpopClaims.ID = "jti-bind-2"
	dpopClaims.IssuedAt = jwt.NewNumericDate(time.Now())

	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	req := makeBindRequest(t, "nonexistent", dpopProof)
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestBindHandler_ExpiredBindCode(t *testing.T) {
	ts := newBindTestSetup(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	bindCode := "bind-expired"
	expires := time.Now().Add(-1 * time.Minute)
	ts.store.addSession(bindCode, &BindSession{
		ID:              "sess-2",
		TenantID:        "tenant-1",
		Status:          session.StatusActive,
		Scopes:          []string{"cart.view"},
		BindCodeExpires: &expires,
		ExpiresAt:       time.Now().Add(time.Hour),
	})

	dpopClaims := DPoPClaims{
		HTM: "POST",
		HTU: "http://localhost/token/bind",
	}
	dpopClaims.ID = "jti-bind-3"
	dpopClaims.IssuedAt = jwt.NewNumericDate(time.Now())

	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	req := makeBindRequest(t, bindCode, dpopProof)
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestBindHandler_AlreadyUsedBindCode(t *testing.T) {
	ts := newBindTestSetup(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	bindCode := "bind-used"
	ts.store.addSession(bindCode, &BindSession{
		ID:              "sess-3",
		TenantID:        "tenant-1",
		Status:          session.StatusActive,
		Scopes:          []string{"cart.view"},
		BindCodeExpires: nil, // already used
		ExpiresAt:       time.Now().Add(time.Hour),
	})

	dpopClaims := DPoPClaims{
		HTM: "POST",
		HTU: "http://localhost/token/bind",
	}
	dpopClaims.ID = "jti-bind-4"
	dpopClaims.IssuedAt = jwt.NewNumericDate(time.Now())

	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	req := makeBindRequest(t, bindCode, dpopProof)
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestBindHandler_SessionNotActive(t *testing.T) {
	ts := newBindTestSetup(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	bindCode := "bind-suspended"
	expires := time.Now().Add(time.Hour)
	ts.store.addSession(bindCode, &BindSession{
		ID:              "sess-4",
		TenantID:        "tenant-1",
		Status:          session.StatusSuspended,
		Scopes:          []string{"cart.view"},
		BindCodeExpires: &expires,
		ExpiresAt:       expires,
	})

	dpopClaims := DPoPClaims{
		HTM: "POST",
		HTU: "http://localhost/token/bind",
	}
	dpopClaims.ID = "jti-bind-5"
	dpopClaims.IssuedAt = jwt.NewNumericDate(time.Now())

	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	req := makeBindRequest(t, bindCode, dpopProof)
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestBindHandler_InvalidDPoPProof(t *testing.T) {
	ts := newBindTestSetup(t)

	bindCode := "bind-bad-dpop"
	expires := time.Now().Add(time.Hour)
	ts.store.addSession(bindCode, &BindSession{
		ID:              "sess-5",
		TenantID:        "tenant-1",
		Status:          session.StatusActive,
		Scopes:          []string{"cart.view"},
		BindCodeExpires: &expires,
		ExpiresAt:       expires,
	})

	req := makeBindRequest(t, bindCode, "not-a-valid-jwt")
	rec := httptest.NewRecorder()

	ts.handler.Bind(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestBindHandler_DPoPReplayNonce(t *testing.T) {
	ts := newBindTestSetup(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	bindCode := "bind-replay"
	expires := time.Now().Add(time.Hour)
	ts.store.addSession(bindCode, &BindSession{
		ID:              "sess-6",
		TenantID:        "tenant-1",
		Status:          session.StatusActive,
		Scopes:          []string{"cart.view"},
		BindCodeExpires: &expires,
		ExpiresAt:       expires,
	})

	dpopClaims := DPoPClaims{
		HTM: "POST",
		HTU: "http://localhost/token/bind",
	}
	dpopClaims.ID = "jti-replay-1"
	dpopClaims.IssuedAt = jwt.NewNumericDate(time.Now())

	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	// First request should succeed.
	req1 := makeBindRequest(t, bindCode, dpopProof)
	rec1 := httptest.NewRecorder()
	ts.handler.Bind(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", rec1.Code)
	}

	// Add a new session with the same bind code pattern for the second request.
	// Actually the bind code was consumed, so we'd need a different session.
	// Let's just test the DPoP nonce directly with a different bind code.
	bindCode2 := "bind-replay-2"
	ts.store.addSession(bindCode2, &BindSession{
		ID:              "sess-7",
		TenantID:        "tenant-1",
		Status:          session.StatusActive,
		Scopes:          []string{"cart.view"},
		BindCodeExpires: &expires,
		ExpiresAt:       expires,
	})

	req2 := makeBindRequest(t, bindCode2, dpopProof)
	rec2 := httptest.NewRecorder()
	ts.handler.Bind(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for replayed DPoP nonce, got %d", rec2.Code)
	}
}
