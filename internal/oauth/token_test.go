package oauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/token"
	"github.com/golang-jwt/jwt/v5"
)

// testKeyManager is a minimal key manager for tests.
type testKeyManager struct {
	masterKey []byte
}

func newTestKeyManager() *testKeyManager {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &testKeyManager{masterKey: key}
}

func (km *testKeyManager) SignJWT(_ context.Context, keys crypto.TenantKeys, payload []byte) ([]byte, error) {
	// Decrypt private key using simple XOR for test (actual impl uses AES-GCM).
	// For tests, we store the raw private key in ArxSigningPrivateKeyEnc.
	privKey := ed25519.PrivateKey(keys.ArxSigningPrivateKeyEnc)
	return ed25519.Sign(privKey, payload), nil
}

func (km *testKeyManager) EncryptSessionData(_ context.Context, _ []byte, plaintext []byte) ([]byte, error) {
	return plaintext, nil
}

func (km *testKeyManager) DecryptSessionData(_ context.Context, _ []byte, ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}

func (km *testKeyManager) SignRequest(_ context.Context, _ crypto.TenantKeys, _ crypto.SignedPayload) ([]byte, error) {
	return nil, nil
}

func (km *testKeyManager) VerifyWebhook(_ context.Context, _ crypto.TenantKeys, _ crypto.SignedPayload, _ []byte) error {
	return nil
}
func (km *testKeyManager) GenerateDEK(_ context.Context) ([]byte, error) { return nil, nil }
func (km *testKeyManager) GenerateSigningKeypair(_ context.Context, _ []byte) (ed25519.PublicKey, []byte, error) {
	return nil, nil, nil
}

// generateTestKeys creates an Ed25519 keypair for testing.
func generateTestKeys() (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return pub, priv
}

// buildDPoPProof creates a minimal valid DPoP proof for testing.
func buildDPoPProof(t *testing.T, method, uri string) (string, ed25519.PublicKey) {
	t.Helper()
	pub, priv := generateTestKeys()

	xBytes := pub[:]
	xB64 := base64.RawURLEncoding.EncodeToString(xBytes)

	claims := token.DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       fmt.Sprintf("jti-%d", time.Now().UnixNano()),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: method,
		HTU: uri,
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["jwk"] = map[string]string{
		"kty": "OKP",
		"crv": "Ed25519",
		"x":   xB64,
	}

	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign dpop: %v", err)
	}
	return signed, pub
}

func TestToken_AuthorizationCode_Success(t *testing.T) {
	pub, priv := generateTestKeys()

	km := newTestKeyManager()
	issuer := token.NewIssuer(token.Config{
		Issuer:          "http://localhost:8080",
		Audience:        "arx",
		AccessTokenTTL:  1 * time.Hour,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}, km)

	flowStore := newMockFlowStore()
	sessStore := newMockSessionStore()

	// Create a code_verifier and code_challenge.
	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])

	// Create a connected flow with a session.
	sessStore.sessions["sess-1"] = testSession("sess-1", "tenant-1")

	flowStore.flows["test-auth-code"] = &PendingFlow{
		UUID:                "flow-uuid",
		TenantID:            "tenant-1",
		ClientID:            "tenant-1",
		RedirectURI:         "http://llm.example.com/callback",
		State:               "state-abc",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		AuthCode:            "test-auth-code",
		SessionID:           "sess-1",
		Status:              "connected",
		ExpiresAt:           time.Now().Add(5 * time.Minute),
	}

	// Key lookup returns tenant keys with the test keypair.
	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{
			ArxSigningPublicKey:     pub,
			ArxSigningPrivateKeyEnc: priv, // In tests, we store raw privkey.
		}, nil
	}

	// We pass nil for nonces (DPoP nonce check returns true).
	h := NewHandler(flowStore, sessStore, nil, nil, keyLookup, issuer, nil)

	// Build DPoP proof.
	dpopProof, _ := buildDPoPProof(t, "POST", "http://localhost:8080/oauth/token")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "test-auth-code")
	form.Set("code_verifier", codeVerifier)

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", dpopProof)
	rec := httptest.NewRecorder()

	h.Token(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["token_type"] != "DPoP" {
		t.Errorf("token_type = %v, want DPoP", resp["token_type"])
	}
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Error("missing access_token")
	}
	if resp["refresh_token"] == nil || resp["refresh_token"] == "" {
		t.Error("missing refresh_token")
	}
	if resp["expires_in"] == nil {
		t.Error("missing expires_in")
	}
}

func TestToken_AuthorizationCode_InvalidPKCE(t *testing.T) {
	pub, priv := generateTestKeys()
	km := newTestKeyManager()
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx", AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour}, km)

	flowStore := newMockFlowStore()
	sessStore := newMockSessionStore()
	sessStore.sessions["sess-1"] = testSession("sess-1", "tenant-1")

	flowStore.flows["test-code"] = &PendingFlow{
		UUID:          "flow-uuid",
		TenantID:      "tenant-1",
		CodeChallenge: "correct-challenge-hash",
		SessionID:     "sess-1",
		Status:        "connected",
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub, ArxSigningPrivateKeyEnc: priv}, nil
	}

	h := NewHandler(flowStore, sessStore, nil, nil, keyLookup, issuer, nil)
	dpopProof, _ := buildDPoPProof(t, "POST", "http://localhost:8080/oauth/token")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "test-code")
	form.Set("code_verifier", "wrong-verifier")

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", dpopProof)
	rec := httptest.NewRecorder()

	h.Token(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "invalid_grant")
}

func TestToken_RefreshToken_Success(t *testing.T) {
	pub, priv := generateTestKeys()
	km := newTestKeyManager()
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx", AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour}, km)

	sessStore := newMockSessionStore()
	sessStore.sessions["sess-1"] = testSession("sess-1", "tenant-1")

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub, ArxSigningPrivateKeyEnc: priv}, nil
	}

	h := NewHandler(newMockFlowStore(), sessStore, nil, nil, keyLookup, issuer, nil)

	// Issue a real refresh token to use.
	keys := crypto.TenantKeys{ArxSigningPublicKey: pub, ArxSigningPrivateKeyEnc: priv}
	refreshTokenStr, err := issuer.IssueRefreshToken(context.Background(), keys, "sess-1", "tenant-1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshTokenStr)

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Token(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["token_type"] != "DPoP" {
		t.Errorf("token_type = %v, want DPoP", resp["token_type"])
	}
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Error("missing access_token")
	}
	if resp["refresh_token"] == nil || resp["refresh_token"] == "" {
		t.Error("missing refresh_token")
	}

	// Verify session refresh count was incremented.
	sess := sessStore.sessions["sess-1"]
	if sess.RefreshCount != 1 {
		t.Errorf("refresh_count = %d, want 1", sess.RefreshCount)
	}
}

func TestToken_RefreshToken_MaxRefreshes(t *testing.T) {
	pub, priv := generateTestKeys()
	km := newTestKeyManager()
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx", AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour}, km)

	sessStore := newMockSessionStore()
	sess := testSession("sess-1", "tenant-1")
	sess.RefreshCount = 5 // At the max.
	sessStore.sessions["sess-1"] = sess

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub, ArxSigningPrivateKeyEnc: priv}, nil
	}

	h := NewHandler(newMockFlowStore(), sessStore, nil, nil, keyLookup, issuer, nil)

	keys := crypto.TenantKeys{ArxSigningPublicKey: pub, ArxSigningPrivateKeyEnc: priv}
	refreshTokenStr, _ := issuer.IssueRefreshToken(context.Background(), keys, "sess-1", "tenant-1", time.Now().Add(time.Hour))

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshTokenStr)

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Token(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "invalid_grant")
}

func TestToken_RefreshToken_RevokedSession(t *testing.T) {
	pub, priv := generateTestKeys()
	km := newTestKeyManager()
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx", AccessTokenTTL: time.Hour, RefreshTokenTTL: 30 * 24 * time.Hour}, km)

	sessStore := newMockSessionStore()
	sess := testSession("sess-1", "tenant-1")
	sess.Status = session.StatusRevoked
	sessStore.sessions["sess-1"] = sess

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub, ArxSigningPrivateKeyEnc: priv}, nil
	}

	h := NewHandler(newMockFlowStore(), sessStore, nil, nil, keyLookup, issuer, nil)

	keys := crypto.TenantKeys{ArxSigningPublicKey: pub, ArxSigningPrivateKeyEnc: priv}
	refreshTokenStr, _ := issuer.IssueRefreshToken(context.Background(), keys, "sess-1", "tenant-1", time.Now().Add(time.Hour))

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshTokenStr)

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Token(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "invalid_grant")
}

func TestToken_UnsupportedGrantType(t *testing.T) {
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(nil, nil, nil, nil, nil, issuer, nil)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Token(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertErrorCode(t, rec, "unsupported_grant_type")
}
