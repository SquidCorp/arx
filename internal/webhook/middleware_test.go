package webhook

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
)

// testSetup creates all the dependencies needed for middleware tests.
type testSetup struct {
	merchantPub  ed25519.PublicKey
	merchantPriv ed25519.PrivateKey
	km           crypto.KeyManager
	nonces       *cache.NonceStore
	validator    *SignatureValidator
	tenantID     string
	mini         *miniredis.Miniredis
}

func newTestSetup(t *testing.T) *testSetup {
	t.Helper()

	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}
	km, err := crypto.NewLocalKeyManager(hex.EncodeToString(masterKey))
	if err != nil {
		t.Fatal(err)
	}

	merchantPub, merchantPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

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

	nonceStore := cache.NewNonceStore(cacheClient)
	tenantID := "tenant-123"

	keys := crypto.TenantKeys{MerchantPublicKey: merchantPub}

	lookup := func(_ context.Context, id string) (crypto.TenantKeys, error) {
		if id == tenantID {
			return keys, nil
		}
		return crypto.TenantKeys{}, context.DeadlineExceeded
	}

	sv := NewSignatureValidator(lookup, km, nonceStore)

	return &testSetup{
		merchantPub:  merchantPub,
		merchantPriv: merchantPriv,
		km:           km,
		nonces:       nonceStore,
		validator:    sv,
		tenantID:     tenantID,
		mini:         mini,
	}
}

func (ts *testSetup) signRequest(t *testing.T, method, path, body, timestamp, nonce string) string {
	t.Helper()
	payload := crypto.SignedPayload{
		Method:    method,
		Path:      path,
		Body:      []byte(body),
		Timestamp: timestamp,
		Nonce:     nonce,
	}
	sig := ed25519.Sign(ts.merchantPriv, payload.CanonicalMessage())
	return base64.StdEncoding.EncodeToString(sig)
}

func TestMiddleware_ValidSignature(t *testing.T) {
	ts := newTestSetup(t)
	now := time.Now()
	ts.validator.now = func() time.Time { return now }

	body := `{"sessionId":"s1"}`
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "nonce-valid-1"
	sig := ts.signRequest(t, "POST", "/webhook/session-connected", body, timestamp, nonce)

	var capturedTenantID string
	var capturedBody []byte
	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTenantID, _ = TenantIDFromContext(r.Context())
		capturedBody, _ = BodyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req.Header.Set(headerTenantID, ts.tenantID)
	req.Header.Set(headerSignature, sig)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerNonce, nonce)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedTenantID != ts.tenantID {
		t.Errorf("expected tenant ID %q, got %q", ts.tenantID, capturedTenantID)
	}
	if string(capturedBody) != body {
		t.Errorf("expected body %q, got %q", body, string(capturedBody))
	}
}

func TestMiddleware_MissingTenantID(t *testing.T) {
	ts := newTestSetup(t)
	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "missing_tenant_id")
}

func TestMiddleware_MissingSignature(t *testing.T) {
	ts := newTestSetup(t)
	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", nil)
	req.Header.Set(headerTenantID, ts.tenantID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "missing_signature")
}

func TestMiddleware_StaleTimestamp(t *testing.T) {
	ts := newTestSetup(t)
	now := time.Now()
	ts.validator.now = func() time.Time { return now }

	staleTime := now.Add(-6 * time.Minute)
	timestamp := strconv.FormatInt(staleTime.Unix(), 10)
	nonce := "nonce-stale"
	body := `{}`
	sig := ts.signRequest(t, "POST", "/webhook/session-connected", body, timestamp, nonce)

	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req.Header.Set(headerTenantID, ts.tenantID)
	req.Header.Set(headerSignature, sig)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerNonce, nonce)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "stale_timestamp")
}

func TestMiddleware_FutureTimestampWithinWindow(t *testing.T) {
	ts := newTestSetup(t)
	now := time.Now()
	ts.validator.now = func() time.Time { return now }

	futureTime := now.Add(3 * time.Minute)
	timestamp := strconv.FormatInt(futureTime.Unix(), 10)
	nonce := "nonce-future-ok"
	body := `{}`
	sig := ts.signRequest(t, "POST", "/webhook/session-connected", body, timestamp, nonce)

	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req.Header.Set(headerTenantID, ts.tenantID)
	req.Header.Set(headerSignature, sig)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerNonce, nonce)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMiddleware_DuplicateNonce(t *testing.T) {
	ts := newTestSetup(t)
	now := time.Now()
	ts.validator.now = func() time.Time { return now }

	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "nonce-dup"
	body := `{}`
	sig := ts.signRequest(t, "POST", "/webhook/session-connected", body, timestamp, nonce)

	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request — should succeed.
	req1 := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req1.Header.Set(headerTenantID, ts.tenantID)
	req1.Header.Set(headerSignature, sig)
	req1.Header.Set(headerTimestamp, timestamp)
	req1.Header.Set(headerNonce, nonce)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec1.Code)
	}

	// Second request with same nonce — should be rejected.
	req2 := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req2.Header.Set(headerTenantID, ts.tenantID)
	req2.Header.Set(headerSignature, sig)
	req2.Header.Set(headerTimestamp, timestamp)
	req2.Header.Set(headerNonce, nonce)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("duplicate nonce: expected 401, got %d", rec2.Code)
	}
	assertErrorCode(t, rec2, "duplicate_nonce")
}

func TestMiddleware_InvalidSignature(t *testing.T) {
	ts := newTestSetup(t)
	now := time.Now()
	ts.validator.now = func() time.Time { return now }

	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "nonce-invalid-sig"
	body := `{"sessionId":"s1"}`

	// Sign with a different key.
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := crypto.SignedPayload{
		Method:    "POST",
		Path:      "/webhook/session-connected",
		Body:      []byte(body),
		Timestamp: timestamp,
		Nonce:     nonce,
	}
	wrongSig := base64.StdEncoding.EncodeToString(ed25519.Sign(wrongPriv, payload.CanonicalMessage()))

	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req.Header.Set(headerTenantID, ts.tenantID)
	req.Header.Set(headerSignature, wrongSig)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerNonce, nonce)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_signature")
}

func TestMiddleware_UnknownTenant(t *testing.T) {
	ts := newTestSetup(t)
	now := time.Now()
	ts.validator.now = func() time.Time { return now }

	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "nonce-unknown-tenant"
	body := `{}`
	sig := ts.signRequest(t, "POST", "/webhook/session-connected", body, timestamp, nonce)

	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req.Header.Set(headerTenantID, "non-existent-tenant")
	req.Header.Set(headerSignature, sig)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerNonce, nonce)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "unknown_tenant")
}

func TestMiddleware_TenantKeysInContext(t *testing.T) {
	ts := newTestSetup(t)
	now := time.Now()
	ts.validator.now = func() time.Time { return now }

	body := `{"sessionId":"s1"}`
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "nonce-ctx-keys"
	sig := ts.signRequest(t, "POST", "/webhook/session-connected", body, timestamp, nonce)

	var gotKeys bool
	handler := ts.validator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys, ok := TenantKeysFromContext(r.Context())
		gotKeys = ok && keys.MerchantPublicKey != nil
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/webhook/session-connected", strings.NewReader(body))
	req.Header.Set(headerTenantID, ts.tenantID)
	req.Header.Set(headerSignature, sig)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerNonce, nonce)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !gotKeys {
		t.Error("expected TenantKeys in context")
	}
}

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
