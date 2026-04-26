package token

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/golang-jwt/jwt/v5"
)

// Config holds configuration for token issuance.
type Config struct {
	// Issuer is the `iss` claim (Arx URL).
	Issuer string
	// Audience is the `aud` claim.
	Audience string
	// AccessTokenTTL is the default lifetime for access tokens.
	AccessTokenTTL time.Duration
	// RefreshTokenTTL is the default lifetime for refresh tokens.
	RefreshTokenTTL time.Duration
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig(issuer string) Config {
	return Config{
		Issuer:          issuer,
		Audience:        "arx",
		AccessTokenTTL:  1 * time.Hour,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}
}

// AccessTokenClaims represents the claims in a DPoP-bound access token.
type AccessTokenClaims struct {
	jwt.RegisteredClaims
	// Cnf holds the confirmation method (JWK thumbprint).
	Cnf struct {
		JKT string `json:"jkt"`
	} `json:"cnf"`
	// Scope is a space-separated list of granted scopes.
	Scope string `json:"scope"`
	// Tenant is the tenant ID.
	Tenant string `json:"tenant"`
}

// RefreshTokenClaims represents the claims in a refresh token.
type RefreshTokenClaims struct {
	jwt.RegisteredClaims
	// SessionID references the session this refresh token belongs to.
	SessionID string `json:"sid"`
	// Tenant is the tenant ID.
	Tenant string `json:"tenant"`
}

// Issuer handles JWT access and refresh token creation.
type Issuer struct {
	config     Config
	keyManager crypto.KeyManager
}

// NewIssuer creates a new token Issuer.
func NewIssuer(config Config, keyManager crypto.KeyManager) *Issuer {
	return &Issuer{
		config:     config,
		keyManager: keyManager,
	}
}

// Config returns the issuer's configuration.
func (i *Issuer) Config() Config {
	return i.config
}

// IssueAccessToken creates a new DPoP-bound access token.
// The token is signed with the tenant's Ed25519 signing key.
func (i *Issuer) IssueAccessToken(ctx context.Context, keys crypto.TenantKeys, sessionID, tenantID, thumbprint string, scopes []string, expiresAt time.Time) (string, error) {
	now := NowFunc()

	// Use the earlier of the configured TTL or the session expiry.
	ttl := i.config.AccessTokenTTL
	if until := expiresAt.Sub(now); until > 0 && until < ttl {
		ttl = until
	}

	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.config.Issuer,
			Subject:   sessionID,
			Audience:  jwt.ClaimStrings{i.config.Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Cnf: struct {
			JKT string `json:"jkt"`
		}{
			JKT: thumbprint,
		},
		Scope:  strings.Join(scopes, " "),
		Tenant: tenantID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signingString, err := token.SigningString()
	if err != nil {
		return "", fmt.Errorf("build signing string: %w", err)
	}

	sig, err := i.keyManager.SignJWT(ctx, keys, []byte(signingString))
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}

	return signingString + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// IssueRefreshToken creates a new refresh token.
// The token is signed with the tenant's Ed25519 signing key.
func (i *Issuer) IssueRefreshToken(ctx context.Context, keys crypto.TenantKeys, sessionID, tenantID string, expiresAt time.Time) (string, error) {
	now := NowFunc()

	// Use the earlier of the configured TTL or the session expiry.
	ttl := i.config.RefreshTokenTTL
	if until := expiresAt.Sub(now); until > 0 && until < ttl {
		ttl = until
	}

	claims := RefreshTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.config.Issuer,
			Subject:   sessionID,
			Audience:  jwt.ClaimStrings{i.config.Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		SessionID: sessionID,
		Tenant:    tenantID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signingString, err := token.SigningString()
	if err != nil {
		return "", fmt.Errorf("build signing string: %w", err)
	}

	sig, err := i.keyManager.SignJWT(ctx, keys, []byte(signingString))
	if err != nil {
		return "", fmt.Errorf("sign refresh token: %w", err)
	}

	return signingString + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// ParseAccessToken parses and validates an access token JWT.
// It verifies the signature using the provided Ed25519 public key.
func ParseAccessToken(tokenString string, publicKey []byte) (*AccessTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AccessTokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodEdDSA {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		if len(publicKey) != 32 {
			return nil, fmt.Errorf("invalid ed25519 public key size: %d", len(publicKey))
		}
		return ed25519.PublicKey(publicKey), nil
	}, jwt.WithValidMethods([]string{"EdDSA"}))
	if err != nil {
		return nil, fmt.Errorf("parse access token: %w", err)
	}

	claims, ok := token.Claims.(*AccessTokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid access token claims")
	}

	return claims, nil
}

// ParseRefreshTokenUnverified parses a refresh token without signature verification.
// Used to extract the tenant ID before looking up signing keys.
func ParseRefreshTokenUnverified(tokenString string) (*RefreshTokenClaims, error) {
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, &RefreshTokenClaims{})
	if err != nil {
		return nil, fmt.Errorf("parse refresh token unverified: %w", err)
	}

	claims, ok := token.Claims.(*RefreshTokenClaims)
	if !ok {
		return nil, fmt.Errorf("invalid refresh token claims")
	}

	return claims, nil
}

// ParseRefreshToken parses and validates a refresh token JWT.
// It verifies the signature using the provided Ed25519 public key.
func ParseRefreshToken(tokenString string, publicKey []byte) (*RefreshTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &RefreshTokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodEdDSA {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		if len(publicKey) != 32 {
			return nil, fmt.Errorf("invalid ed25519 public key size: %d", len(publicKey))
		}
		return ed25519.PublicKey(publicKey), nil
	}, jwt.WithValidMethods([]string{"EdDSA"}))
	if err != nil {
		return nil, fmt.Errorf("parse refresh token: %w", err)
	}

	claims, ok := token.Claims.(*RefreshTokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid refresh token claims")
	}

	return claims, nil
}
