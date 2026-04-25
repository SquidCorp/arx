package token

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/golang-jwt/jwt/v5"
)

// noOpNonceCheck accepts every DPoP nonce for tests that do not reach DPoP validation.
func noOpNonceCheck(_ string) (bool, error) { return true, nil }

// mockSessionReader is an in-memory SessionReader for testing.
type mockSessionReader struct {
	sessions map[string]*SessionInfo
}

func newMockSessionReader() *mockSessionReader {
	return &mockSessionReader{sessions: make(map[string]*SessionInfo)}
}

func (m *mockSessionReader) GetSession(_ context.Context, id string) (*SessionInfo, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, context.Canceled // any error
	}
	return s, nil
}

func (m *mockSessionReader) addSession(s *SessionInfo) {
	m.sessions[s.ID] = s
}

// mockTenantKeyLookup returns fixed tenant keys for testing.
func mockTenantKeyLookup(keys crypto.TenantKeys) TenantKeyLookup {
	return func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return keys, nil
	}
}

func TestValidatorMiddleware_ValidToken(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-1",
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		Scopes:    []string{"cart.view"},
		ExpiresAt: time.Now().Add(time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()

	// Generate a keypair so the DPoP proof thumbprint matches the token.
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	jwkMap := jwk{Kty: "OKP", Crv: "Ed25519", X: base64.RawURLEncoding.EncodeToString(pub)}
	thumbprint, err := jwkThumbprint(jwkMap)
	if err != nil {
		t.Fatal(err)
	}

	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-1", "tenant-1", thumbprint, []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256([]byte(tokenStr))
	ath := base64.RawURLEncoding.EncodeToString(hash[:])

	dpopClaims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-mw-1",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "GET",
		HTU: "http://localhost/mcp",
		ATH: ath,
	}
	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	req := httptest.NewRequest("GET", "http://localhost/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	req.Header.Set("DPoP", dpopProof)
	rec := httptest.NewRecorder()

	var gotSessionID, gotTenantID string
	var gotScopes []string

	handler := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSessionID, _ = SessionIDFromContext(r.Context())
		gotTenantID, _ = TenantIDFromContext(r.Context())
		gotScopes, _ = ScopesFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotSessionID != "sess-1" {
		t.Errorf("sessionID: want %q, got %q", "sess-1", gotSessionID)
	}
	if gotTenantID != "tenant-1" {
		t.Errorf("tenantID: want %q, got %q", "tenant-1", gotTenantID)
	}
	if len(gotScopes) != 1 || gotScopes[0] != "cart.view" {
		t.Errorf("scopes: want [cart.view], got %v", gotScopes)
	}
}

func TestValidatorMiddleware_MissingAuthorization(t *testing.T) {
	v := NewValidator(nil, nil, "https://arx.example.com", "arx", noOpNonceCheck)

	req := httptest.NewRequest("GET", "/mcp", nil)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_InvalidTokenFormat(t *testing.T) {
	v := NewValidator(nil, nil, "https://arx.example.com", "arx", noOpNonceCheck)

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP not-a-jwt")
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_WrongSignature(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	// Use different keys for validation.
	otherPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	otherKeys := crypto.TenantKeys{ArxSigningPublicKey: otherPub}

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-1",
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(otherKeys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-1", "tenant-1", "thumb", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_SessionExpired(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-expired",
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-expired", "tenant-1", "thumb", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_SessionRevoked(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-revoked",
		TenantID:  "tenant-1",
		Status:    session.StatusRevoked,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-revoked", "tenant-1", "thumb", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_SessionSuspended(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-suspended",
		TenantID:  "tenant-1",
		Status:    session.StatusSuspended,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-suspended", "tenant-1", "thumb", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_InvalidIssuer(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-1",
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	// Validator expects a different issuer.
	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://evil.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-1", "tenant-1", "thumb", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_InvalidAudience(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)

	// Issue a token with wrong audience by manually creating claims.
	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://arx.example.com",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"other"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Cnf: struct {
			JKT string `json:"jkt"`
		}{JKT: "thumb"},
		Scope:  "cart.view",
		Tenant: "tenant-1",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signingString, err := token.SigningString()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := km.SignJWT(t.Context(), keys, []byte(signingString))
	if err != nil {
		t.Fatal(err)
	}
	tokenStr := signingString + "." + base64.RawURLEncoding.EncodeToString(sig)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-1",
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_MissingDPoPProof(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-1",
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		Scopes:    []string{"cart.view"},
		ExpiresAt: time.Now().Add(time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-1", "tenant-1", "thumb", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestValidatorMiddleware_DPoPBindingMismatch(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	reader := newMockSessionReader()
	reader.addSession(&SessionInfo{
		ID:        "sess-1",
		TenantID:  "tenant-1",
		Status:    session.StatusActive,
		Scopes:    []string{"cart.view"},
		ExpiresAt: time.Now().Add(time.Hour),
	})

	v := NewValidator(mockTenantKeyLookup(keys), reader, "https://arx.example.com", "arx", noOpNonceCheck)

	ctx := t.Context()

	// Token issued with one thumbprint.
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-1", "tenant-1", "thumb-1", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	// DPoP proof signed with a different key (different thumbprint).
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256([]byte(tokenStr))
	ath := base64.RawURLEncoding.EncodeToString(hash[:])

	dpopClaims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-mw-dpop-mismatch",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "GET",
		HTU: "http://localhost/mcp",
		ATH: ath,
	}
	dpopProof := generateDPoPProof(t, priv, pub, dpopClaims)

	req := httptest.NewRequest("GET", "http://localhost/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)
	req.Header.Set("DPoP", dpopProof)
	rec := httptest.NewRecorder()

	handler := v.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
