// Package token provides DPoP proof validation, JWT issuance, and access token
// validation middleware.
//
// This file contains end-to-end tests for the full DPoP token flow: bind-code
// exchange, access-token issuance, and DPoP-bound resource access.
package token

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

// dpopE2ESetup holds all dependencies for the DPoP end-to-end tests.
type dpopE2ESetup struct {
	router        chi.Router
	store         *mockBindSessionStore
	sessionReader *mockSessionReader
	mini          *miniredis.Miniredis
	keys          crypto.TenantKeys
	issuer        *Issuer
	dpopPub       ed25519.PublicKey
	dpopPriv      ed25519.PrivateKey
	dpopPubB      ed25519.PublicKey
	dpopPrivB     ed25519.PrivateKey
}

// newDPoPE2ESetup creates a fully wired DPoP test environment.
func newDPoPE2ESetup(t *testing.T) *dpopE2ESetup {
	t.Helper()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mini.Close)

	cacheClient, err := cache.Connect(t.Context(), "redis://"+mini.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cacheClient.Close() })

	sessionCache := cache.NewSessionCache(cacheClient)
	store := newMockBindSessionStore()
	sessionReader := newMockSessionReader()

	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)
	nonces := cache.NewNonceStore(cacheClient)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return keys, nil
	}

	bindHandler := NewBindHandler(store, sessionCache, keyLookup, issuer, nonces)
	validator := NewValidator(keyLookup, sessionReader, "https://arx.example.com", "arx", func(jti string) (bool, error) {
		return nonces.CheckDPoPNonce(t.Context(), jti)
	})

	dpopPub, dpopPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	dpopPubB, dpopPrivB, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	resourceHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r := chi.NewRouter()
	r.Post("/token/bind", bindHandler.Bind)
	r.Post("/token/bind/", bindHandler.Bind)
	r.Get("/resource", validator.Middleware(resourceHandler).ServeHTTP)

	return &dpopE2ESetup{
		router:        r,
		store:         store,
		sessionReader: sessionReader,
		mini:          mini,
		keys:          keys,
		issuer:        issuer,
		dpopPub:       dpopPub,
		dpopPriv:      dpopPriv,
		dpopPubB:      dpopPubB,
		dpopPrivB:     dpopPrivB,
	}
}

// addActiveSession seeds an active session with a bind code.
func (s *dpopE2ESetup) addActiveSession(t *testing.T, sessID, bindCode string, expiresAt time.Time) {
	t.Helper()
	s.store.addSession(bindCode, &BindSession{
		ID:              sessID,
		TenantID:        "tenant-1",
		Status:          session.StatusActive,
		Scopes:          []string{"cart.view", "cart.add"},
		BindCodeExpires: &expiresAt,
		ExpiresAt:       expiresAt,
	})
	s.sessionReader.addSession(&SessionInfo{
		ID:        sessID,
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		Scopes:    []string{"cart.view", "cart.add"},
		ExpiresAt: expiresAt,
	})
}

// makeDPoPProof creates a DPoP proof JWT with the setup's key A.
func (s *dpopE2ESetup) makeDPoPProof(t *testing.T, htm, htu, ath, jti string, iat time.Time) string {
	t.Helper()
	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       jti,
			IssuedAt: jwt.NewNumericDate(iat),
		},
		HTM: htm,
		HTU: htu,
		ATH: ath,
	}
	return generateDPoPProof(t, s.dpopPriv, s.dpopPub, claims)
}

// makeDPoPProofWithKeyB creates a DPoP proof JWT signed with key B.
func (s *dpopE2ESetup) makeDPoPProofWithKeyB(t *testing.T, htm, htu, ath, jti string, iat time.Time) string {
	t.Helper()
	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       jti,
			IssuedAt: jwt.NewNumericDate(iat),
		},
		HTM: htm,
		HTU: htu,
		ATH: ath,
	}
	return generateDPoPProof(t, s.dpopPrivB, s.dpopPubB, claims)
}

// bind performs a POST /token/bind and returns the response.
func (s *dpopE2ESetup) bind(t *testing.T, bindCode, dpopProof string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"bind_code":  bindCode,
		"dpop_proof": dpopProof,
	})
	req := httptest.NewRequest(http.MethodPost, "http://localhost/token/bind", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// callResource performs a GET /resource with the given token and DPoP proof.
func (s *dpopE2ESetup) callResource(t *testing.T, token, dpopProof string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://localhost/resource", nil)
	req.Header.Set("Authorization", "DPoP "+token)
	req.Header.Set("DPoP", dpopProof)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// bindAndGetToken binds a session and returns the access token.
func (s *dpopE2ESetup) bindAndGetToken(t *testing.T, bindCode string) string {
	t.Helper()
	jti := fmt.Sprintf("jti-%d", time.Now().UnixNano())
	dpopProof := s.makeDPoPProof(t, "POST", "http://localhost/token/bind", "", jti, time.Now())
	rec := s.bind(t, bindCode, dpopProof)
	if rec.Code != http.StatusOK {
		t.Fatalf("bind expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp bindResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.AccessToken
}

func TestDPoPE2E_BindAndUseToken(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-happy", "bind-happy", expires)

	// 1.1 Bind: POST /token/bind with valid bind_code + DPoP proof.
	jti := "jti-happy-1"
	dpopProof := ts.makeDPoPProof(t, "POST", "http://localhost/token/bind", "", jti, time.Now())
	rec := ts.bind(t, "bind-happy", dpopProof)

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

	// 1.2 Use issued token on resource endpoint.
	hash := sha256.Sum256([]byte(resp.AccessToken))
	ath := base64.RawURLEncoding.EncodeToString(hash[:])
	resourceDPoP := ts.makeDPoPProof(t, "GET", "http://localhost/resource", ath, "jti-happy-2", time.Now())
	resourceRec := ts.callResource(t, resp.AccessToken, resourceDPoP)

	if resourceRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on resource, got %d: %s", resourceRec.Code, resourceRec.Body.String())
	}
}

func TestDPoPE2E_TokenBoundToWrongKey(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-wrong-key", "bind-wrong-key", expires)

	token := ts.bindAndGetToken(t, "bind-wrong-key")

	hash := sha256.Sum256([]byte(token))
	ath := base64.RawURLEncoding.EncodeToString(hash[:])
	// Sign DPoP proof with key B instead of key A.
	resourceDPoP := ts.makeDPoPProofWithKeyB(t, "GET", "http://localhost/resource", ath, "jti-wrong-key", time.Now())
	rec := ts.callResource(t, token, resourceDPoP)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.String(), "dpop_binding_mismatch")
}

func TestDPoPE2E_MissingDPoPProofOnBind(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-missing-dpop", "bind-missing-dpop", expires)

	body, _ := json.Marshal(map[string]string{
		"bind_code": "bind-missing-dpop",
	})
	req := httptest.NewRequest(http.MethodPost, "/token/bind", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.String(), "missing_dpop_proof")
}

func TestDPoPE2E_DPoPProofWrongHTM(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-wrong-htm", "bind-wrong-htm", expires)

	dpopProof := ts.makeDPoPProof(t, "GET", "http://localhost/token/bind", "", "jti-wrong-htm", time.Now())
	rec := ts.bind(t, "bind-wrong-htm", dpopProof)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.String(), "invalid_dpop_proof")
}

func TestDPoPE2E_DPoPProofWrongHTU(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-wrong-htu", "bind-wrong-htu", expires)

	dpopProof := ts.makeDPoPProof(t, "POST", "http://localhost/other/endpoint", "", "jti-wrong-htu", time.Now())
	rec := ts.bind(t, "bind-wrong-htu", dpopProof)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.String(), "invalid_dpop_proof")
}

func TestDPoPE2E_DPoPProofStaleIAT(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-stale", "bind-stale", expires)

	dpopProof := ts.makeDPoPProof(t, "POST", "http://localhost/token/bind", "", "jti-stale", time.Now().Add(-2*time.Minute))
	rec := ts.bind(t, "bind-stale", dpopProof)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.String(), "invalid_dpop_proof")
}

func TestDPoPE2E_DPoPProofReplayedJTI(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-replay-1", "bind-replay-1", expires)
	ts.addActiveSession(t, "sess-replay-2", "bind-replay-2", expires)

	jti := "jti-replay-same"
	dpopProof := ts.makeDPoPProof(t, "POST", "http://localhost/token/bind", "", jti, time.Now())

	// First bind should succeed.
	rec1 := ts.bind(t, "bind-replay-1", dpopProof)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first bind expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// Second bind with same JTI should fail.
	rec2 := ts.bind(t, "bind-replay-2", dpopProof)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for replayed jti, got %d", rec2.Code)
	}
	assertErrorCode(t, rec2.Body.String(), "invalid_dpop_proof")
}

func TestDPoPE2E_HTUWithTrailingSlash(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-trailing", "bind-trailing", expires)

	// Request URL has trailing slash; DPoP proof htu does not.
	body, _ := json.Marshal(map[string]string{
		"bind_code":  "bind-trailing",
		"dpop_proof": ts.makeDPoPProof(t, "POST", "http://localhost/token/bind", "", "jti-trailing", time.Now()),
	})
	req := httptest.NewRequest(http.MethodPost, "http://localhost/token/bind/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for trailing slash normalization, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDPoPE2E_HTUWithPercentEncoding(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	expires := time.Now().Add(time.Hour)
	ts.addActiveSession(t, "sess-percent", "bind-percent", expires)

	// DPoP proof htu has percent-encoded path.
	htu := "http://localhost/token%2Fbind"
	dpopProof := ts.makeDPoPProof(t, "POST", htu, "", "jti-percent", time.Now())
	rec := ts.bind(t, "bind-percent", dpopProof)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for percent-encoded path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDPoPE2E_TokenTTLCappedToSessionExpiry(t *testing.T) {
	ts := newDPoPE2ESetup(t)
	// Session expires in 10 minutes.
	expires := time.Now().Add(10 * time.Minute)
	ts.addActiveSession(t, "sess-short", "bind-short", expires)

	jti := "jti-short-ttl"
	dpopProof := ts.makeDPoPProof(t, "POST", "http://localhost/token/bind", "", jti, time.Now())
	rec := ts.bind(t, "bind-short", dpopProof)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp bindResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// expires_in should be <= 600 seconds (10 minutes) + small clock skew tolerance.
	if resp.ExpiresIn > 605 {
		t.Errorf("expected expires_in <= 605, got %d", resp.ExpiresIn)
	}
}

// assertErrorCode checks that a JSON error response contains the expected error code.
func assertErrorCode(t *testing.T, body, want string) {
	t.Helper()
	var resp map[string]string
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got := resp["error"]; got != want {
		t.Errorf("error code: want %q, got %q", want, got)
	}
}
