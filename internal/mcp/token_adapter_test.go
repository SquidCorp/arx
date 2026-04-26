package mcp_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/mcp"
	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/token"
	"github.com/golang-jwt/jwt/v5"
)

// testKeyPair generates an Ed25519 key pair for testing.
func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

// signTestToken creates a signed JWT access token for testing.
func signTestToken(t *testing.T, priv ed25519.PrivateKey, claims token.AccessTokenClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

type mockSessionReader struct {
	info *token.SessionInfo
	err  error
}

func (m *mockSessionReader) GetSession(_ context.Context, _ string) (*token.SessionInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.info, nil
}

func TestTokenExtractorAdapter_NoAuthHeader(t *testing.T) {
	t.Parallel()

	adapter := mcp.NewTokenExtractorAdapter(nil, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for missing auth header")
	}
}

func TestTokenExtractorAdapter_InvalidToken(t *testing.T) {
	t.Parallel()

	adapter := mcp.NewTokenExtractorAdapter(nil, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP not-a-jwt")

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestTokenExtractorAdapter_Success(t *testing.T) {
	t.Parallel()

	pub, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read cart:write",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub}, nil
	}
	sessionReader := &mockSessionReader{
		info: &token.SessionInfo{
			ID:        "sess-1",
			TenantID:  "tenant-1",
			Status:    session.StatusActive,
			Scopes:    []string{"cart:read", "cart:write"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, sessionReader, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	info, err := adapter.ExtractToken(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.SessionID != "sess-1" {
		t.Errorf("sessionID = %q, want %q", info.SessionID, "sess-1")
	}
	if info.TenantID != "tenant-1" {
		t.Errorf("tenantID = %q, want %q", info.TenantID, "tenant-1")
	}
	if len(info.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(info.Scopes))
	}
}

func TestTokenExtractorAdapter_BearerScheme(t *testing.T) {
	t.Parallel()

	pub, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub}, nil
	}
	sessionReader := &mockSessionReader{
		info: &token.SessionInfo{
			ID:        "sess-1",
			TenantID:  "tenant-1",
			Status:    session.StatusActive,
			Scopes:    []string{"cart:read"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, sessionReader, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)

	info, err := adapter.ExtractToken(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.SessionID != "sess-1" {
		t.Errorf("sessionID = %q, want %q", info.SessionID, "sess-1")
	}
}

func TestTokenExtractorAdapter_WrongIssuer(t *testing.T) {
	t.Parallel()

	pub, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "wrong-issuer",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub}, nil
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestTokenExtractorAdapter_WrongAudience(t *testing.T) {
	t.Parallel()

	pub, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"wrong-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub}, nil
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestTokenExtractorAdapter_ExpiredSession(t *testing.T) {
	t.Parallel()

	pub, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub}, nil
	}
	sessionReader := &mockSessionReader{
		info: &token.SessionInfo{
			ID:        "sess-1",
			TenantID:  "tenant-1",
			Status:    session.StatusActive,
			Scopes:    []string{"cart:read"},
			ExpiresAt: time.Now().Add(-time.Hour), // expired
		},
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, sessionReader, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for expired session")
	}
}

func TestTokenExtractorAdapter_SuspendedSession(t *testing.T) {
	t.Parallel()

	pub, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub}, nil
	}
	sessionReader := &mockSessionReader{
		info: &token.SessionInfo{
			ID:        "sess-1",
			TenantID:  "tenant-1",
			Status:    session.StatusSuspended,
			Scopes:    []string{"cart:read"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, sessionReader, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for suspended session")
	}
}

func TestTokenExtractorAdapter_TenantKeyLookupFails(t *testing.T) {
	t.Parallel()

	_, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{}, fmt.Errorf("tenant not found")
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for tenant key lookup failure")
	}
}

func TestTokenExtractorAdapter_WrongSigningKey(t *testing.T) {
	t.Parallel()

	_, priv := testKeyPair(t)
	pub2, _ := testKeyPair(t) // different key pair

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub2}, nil // wrong key
	}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for wrong signing key")
	}
}

func TestTokenExtractorAdapter_EmptyTenantClaim(t *testing.T) {
	t.Parallel()

	_, priv := testKeyPair(t)

	// Build token with empty tenant claim.
	claims := jwt.MapClaims{
		"iss": "arx",
		"sub": "sess-1",
		"aud": []string{"arx"},
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tokenStr, _ := tok.SignedString(priv)

	adapter := mcp.NewTokenExtractorAdapter(nil, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for empty tenant claim")
	}
}

func TestTokenExtractorAdapter_SessionReaderError(t *testing.T) {
	t.Parallel()

	pub, priv := testKeyPair(t)

	claims := token.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "arx",
			Subject:   "sess-1",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:  "cart:read",
		Tenant: "tenant-1",
	}
	tokenStr := signTestToken(t, priv, claims)

	keyLookup := func(_ context.Context, _ string) (crypto.TenantKeys, error) {
		return crypto.TenantKeys{ArxSigningPublicKey: pub}, nil
	}
	sessionReader := &mockSessionReader{err: fmt.Errorf("session not found")}

	adapter := mcp.NewTokenExtractorAdapter(keyLookup, sessionReader, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "DPoP "+tokenStr)

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for session reader error")
	}
}

// extractBearerToken is tested indirectly via the adapter tests above.
// Test the edge case of unsupported scheme.
func TestTokenExtractorAdapter_UnsupportedScheme(t *testing.T) {
	t.Parallel()

	adapter := mcp.NewTokenExtractorAdapter(nil, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for unsupported auth scheme")
	}
}

func TestTokenExtractorAdapter_EmptyAuthHeader(t *testing.T) {
	t.Parallel()

	adapter := mcp.NewTokenExtractorAdapter(nil, nil, "arx", "arx")
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "")

	_, err := adapter.ExtractToken(req)
	if err == nil {
		t.Fatal("expected error for empty auth header")
	}
}
