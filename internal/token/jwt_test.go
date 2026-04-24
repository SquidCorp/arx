package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/golang-jwt/jwt/v5"
)

func testKeyManager(t *testing.T) crypto.KeyManager {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	km, err := crypto.NewLocalKeyManager(hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return km
}

func testTenantKeys(t *testing.T, km crypto.KeyManager) crypto.TenantKeys {
	t.Helper()
	ctx := t.Context()

	dekEnc, err := km.GenerateDEK(ctx)
	if err != nil {
		t.Fatal(err)
	}

	pub, privEnc, err := km.GenerateSigningKeypair(ctx, dekEnc)
	if err != nil {
		t.Fatal(err)
	}

	return crypto.TenantKeys{
		ArxSigningPublicKey:     pub,
		ArxSigningPrivateKeyEnc: privEnc,
		DEKEnc:                  dekEnc,
	}
}

func TestIssueAccessToken_ContainsRequiredClaims(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-123", "tenant-456", "thumb-abc", []string{"cart.view", "cart.add"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	claims, err := ParseAccessToken(tokenStr, keys.ArxSigningPublicKey)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}

	if claims.Issuer != "https://arx.example.com" {
		t.Errorf("iss: want %q, got %q", "https://arx.example.com", claims.Issuer)
	}
	if claims.Subject != "sess-123" {
		t.Errorf("sub: want %q, got %q", "sess-123", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "arx" {
		t.Errorf("aud: want [arx], got %v", claims.Audience)
	}
	if claims.Cnf.JKT != "thumb-abc" {
		t.Errorf("cnf.jkt: want %q, got %q", "thumb-abc", claims.Cnf.JKT)
	}
	if claims.Scope != "cart.view cart.add" {
		t.Errorf("scope: want %q, got %q", "cart.view cart.add", claims.Scope)
	}
	if claims.Tenant != "tenant-456" {
		t.Errorf("tenant: want %q, got %q", "tenant-456", claims.Tenant)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Before(time.Now()) {
		t.Error("exp should be in the future")
	}
	if claims.IssuedAt == nil || claims.IssuedAt.After(time.Now()) {
		t.Error("iat should be in the past or present")
	}
}

func TestIssueAccessToken_SignatureVerifies(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-123", "tenant-456", "thumb-abc", []string{"cart.view"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	// Parse with the correct public key — should succeed.
	_, err = ParseAccessToken(tokenStr, keys.ArxSigningPublicKey)
	if err != nil {
		t.Fatalf("parse with correct key: %v", err)
	}

	// Parse with a wrong public key — should fail.
	wrongPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseAccessToken(tokenStr, wrongPub)
	if err == nil {
		t.Error("expected error with wrong public key")
	}
}

func TestIssueAccessToken_ExpiresAtSessionExpiry(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	// Session expires in 5 minutes, which is less than the default 1 hour TTL.
	shortExpiry := time.Now().Add(5 * time.Minute)

	ctx := t.Context()
	tokenStr, err := issuer.IssueAccessToken(ctx, keys, "sess-123", "tenant-456", "thumb", []string{"cart.view"}, shortExpiry)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	claims, err := ParseAccessToken(tokenStr, keys.ArxSigningPublicKey)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Token expiry should be close to session expiry (±1 minute).
	if claims.ExpiresAt == nil {
		t.Fatal("missing exp")
	}
	diff := claims.ExpiresAt.Sub(shortExpiry)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("token expiry %v too far from session expiry %v", claims.ExpiresAt, shortExpiry)
	}
}

func TestIssueRefreshToken_ContainsRequiredClaims(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	ctx := t.Context()
	tokenStr, err := issuer.IssueRefreshToken(ctx, keys, "sess-789", "tenant-456", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}

	claims, err := ParseRefreshToken(tokenStr, keys.ArxSigningPublicKey)
	if err != nil {
		t.Fatalf("parse refresh token: %v", err)
	}

	if claims.Issuer != "https://arx.example.com" {
		t.Errorf("iss: want %q, got %q", "https://arx.example.com", claims.Issuer)
	}
	if claims.Subject != "sess-789" {
		t.Errorf("sub: want %q, got %q", "sess-789", claims.Subject)
	}
	if claims.SessionID != "sess-789" {
		t.Errorf("sid: want %q, got %q", "sess-789", claims.SessionID)
	}
	if claims.Tenant != "tenant-456" {
		t.Errorf("tenant: want %q, got %q", "tenant-456", claims.Tenant)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Before(time.Now()) {
		t.Error("exp should be in the future")
	}
}

func TestIssueRefreshToken_SignatureVerifies(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)
	issuer := NewIssuer(DefaultConfig("https://arx.example.com"), km)

	ctx := t.Context()
	tokenStr, err := issuer.IssueRefreshToken(ctx, keys, "sess-789", "tenant-456", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}

	_, err = ParseRefreshToken(tokenStr, keys.ArxSigningPublicKey)
	if err != nil {
		t.Fatalf("parse with correct key: %v", err)
	}

	wrongPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseRefreshToken(tokenStr, wrongPub)
	if err == nil {
		t.Error("expected error with wrong public key")
	}
}

func TestParseAccessToken_Expired(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)

	// Manually create an expired token.
	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://arx.example.com",
			Subject:   "sess-expired",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
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

	_, err = ParseAccessToken(tokenStr, keys.ArxSigningPublicKey)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestParseRefreshToken_Expired(t *testing.T) {
	km := testKeyManager(t)
	keys := testTenantKeys(t, km)

	claims := RefreshTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://arx.example.com",
			Subject:   "sess-expired",
			Audience:  jwt.ClaimStrings{"arx"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
		SessionID: "sess-expired",
		Tenant:    "tenant-1",
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

	_, err = ParseRefreshToken(tokenStr, keys.ArxSigningPublicKey)
	if err == nil {
		t.Error("expected error for expired refresh token")
	}
}
