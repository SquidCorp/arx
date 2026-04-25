// Package token provides DPoP proof validation, JWT issuance, and access token
// validation middleware.
package token

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DPoPClaims represents the expected claims in a DPoP proof JWT.
type DPoPClaims struct {
	jwt.RegisteredClaims
	// HTM is the HTTP method (e.g., "POST").
	HTM string `json:"htm"`
	// HTU is the HTTP URL (e.g., "https://arx.example.com/mcp").
	HTU string `json:"htu"`
	// ATH is the base64url-encoded SHA-256 hash of the access token.
	ATH string `json:"ath,omitempty"`
}

// DPoP validation errors.
var (
	ErrInvalidDPoPProof   = errors.New("invalid_dpop_proof")
	ErrMissingJWK         = errors.New("dpop: missing jwk header")
	ErrInvalidJWK         = errors.New("dpop: invalid jwk")
	ErrThumbprintMismatch = errors.New("dpop: thumbprint mismatch")
	ErrStaleProof         = errors.New("dpop: proof too old")
	ErrNonceReplay        = errors.New("dpop: nonce replay")
	ErrATHMismatch        = errors.New("dpop: ath mismatch")
	ErrHTMMismatch        = errors.New("dpop: htm mismatch")
	ErrHTUMismatch        = errors.New("dpop: htu mismatch")
)

// jwk represents a minimal Ed25519 JWK.
type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
}

// NowFunc allows overriding the current time in tests.
var NowFunc = time.Now

// ValidateDPoPProof validates a DPoP proof JWT.
// It verifies the signature, checks freshness, htm, htu, ath, and jti uniqueness.
// On success it returns the JWK thumbprint (for comparison with token.cnf.jkt).
func ValidateDPoPProof(dpopJWT, accessToken, method, requestURL string, nonceCheck func(jti string) (bool, error)) (string, error) {
	if dpopJWT == "" {
		return "", fmt.Errorf("%w: empty dpop proof", ErrInvalidDPoPProof)
	}

	// Parse without verification first to extract the JWK from the header.
	token, _, err := new(jwt.Parser).ParseUnverified(dpopJWT, &DPoPClaims{})
	if err != nil {
		return "", fmt.Errorf("%w: parse failed: %v", ErrInvalidDPoPProof, err)
	}

	claims, ok := token.Claims.(*DPoPClaims)
	if !ok {
		return "", fmt.Errorf("%w: invalid claims", ErrInvalidDPoPProof)
	}

	// Extract and decode the JWK from the header.
	rawJWK, ok := token.Header["jwk"]
	if !ok {
		return "", fmt.Errorf("%w: %v", ErrInvalidDPoPProof, ErrMissingJWK)
	}

	jwkMap, err := mapToJWK(rawJWK)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidDPoPProof, err)
	}

	if jwkMap.Kty != "OKP" || jwkMap.Crv != "Ed25519" {
		return "", fmt.Errorf("%w: unsupported key type", ErrInvalidDPoPProof)
	}

	pubKey, err := base64.RawURLEncoding.DecodeString(jwkMap.X)
	if err != nil {
		return "", fmt.Errorf("%w: invalid jwk x: %v", ErrInvalidDPoPProof, err)
	}

	if len(pubKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("%w: invalid ed25519 public key size", ErrInvalidDPoPProof)
	}

	// Verify the JWT signature using the public key from the JWK.
	_, err = jwt.Parse(dpopJWT, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodEdDSA {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return ed25519.PublicKey(pubKey), nil
	}, jwt.WithValidMethods([]string{"EdDSA"}))
	if err != nil {
		return "", fmt.Errorf("%w: signature verification failed: %v", ErrInvalidDPoPProof, err)
	}

	// Validate iat freshness (±60 seconds).
	if claims.IssuedAt == nil {
		return "", fmt.Errorf("%w: missing iat", ErrInvalidDPoPProof)
	}

	now := NowFunc()
	diff := now.Sub(claims.IssuedAt.Time)
	if math.Abs(diff.Seconds()) > 60 {
		return "", fmt.Errorf("%w: %v", ErrInvalidDPoPProof, ErrStaleProof)
	}

	// Validate htm.
	if !strings.EqualFold(claims.HTM, method) {
		return "", fmt.Errorf("%w: %v", ErrInvalidDPoPProof, ErrHTMMismatch)
	}

	// Validate htu (ignore fragment, compare origin + path).
	if err := matchHTU(claims.HTU, requestURL); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidDPoPProof, err)
	}

	// Validate ath (SHA-256 of access token) if accessToken is provided.
	if accessToken != "" {
		if claims.ATH == "" {
			return "", fmt.Errorf("%w: missing ath", ErrInvalidDPoPProof)
		}

		hash := sha256.Sum256([]byte(accessToken))
		expectedATH := base64.RawURLEncoding.EncodeToString(hash[:])
		if claims.ATH != expectedATH {
			return "", fmt.Errorf("%w: %v", ErrInvalidDPoPProof, ErrATHMismatch)
		}
	}

	// Validate jti nonce uniqueness.
	if claims.ID == "" {
		return "", fmt.Errorf("%w: missing jti", ErrInvalidDPoPProof)
	}

	if nonceCheck != nil {
		unique, err := nonceCheck(claims.ID)
		if err != nil {
			return "", fmt.Errorf("%w: nonce check failed: %v", ErrInvalidDPoPProof, err)
		}
		if !unique {
			return "", fmt.Errorf("%w: %v", ErrInvalidDPoPProof, ErrNonceReplay)
		}
	}

	// Compute JWK thumbprint (SHA-256 of canonical JSON per RFC 7638).
	thumbprint, err := jwkThumbprint(jwkMap)
	if err != nil {
		return "", fmt.Errorf("%w: thumbprint failed: %v", ErrInvalidDPoPProof, err)
	}

	return thumbprint, nil
}

// mapToJWK converts a raw map from the JWT header into a typed jwk struct.
func mapToJWK(raw interface{}) (jwk, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return jwk{}, ErrInvalidJWK
	}

	var j jwk
	if v, ok := m["kty"].(string); ok {
		j.Kty = v
	}
	if v, ok := m["crv"].(string); ok {
		j.Crv = v
	}
	if v, ok := m["x"].(string); ok {
		j.X = v
	}

	if j.Kty == "" || j.Crv == "" || j.X == "" {
		return jwk{}, ErrInvalidJWK
	}

	return j, nil
}

// jwkThumbprint computes the RFC 7638 JWK thumbprint for an Ed25519 key.
// The canonical JSON is: {"crv":"Ed25519","kty":"OKP","x":"<base64url(x)>"}
func jwkThumbprint(jwkMap jwk) (string, error) {
	canonical := fmt.Sprintf(`{"crv":"%s","kty":"%s","x":"%s"}`, jwkMap.Crv, jwkMap.Kty, jwkMap.X)
	hash := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

// RequestURL reconstructs the full request URL from an http.Request.
// Go's http.Server only populates r.URL with path and query; the host
// and scheme must be inferred from r.Host and r.TLS.
func RequestURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + r.URL.RequestURI()
}

// matchHTU checks whether the claimed HTU matches the request URL.
// It normalises both URLs and compares scheme, host, and path (ignoring fragments).
func matchHTU(claimed, actual string) error {
	claimedURL, err := url.Parse(claimed)
	if err != nil {
		return fmt.Errorf("invalid htu: %w", err)
	}

	actualURL, err := url.Parse(actual)
	if err != nil {
		return fmt.Errorf("invalid request url: %w", err)
	}

	// Normalise hosts to lowercase.
	if !strings.EqualFold(claimedURL.Host, actualURL.Host) {
		return ErrHTUMismatch
	}
	if claimedURL.Scheme != actualURL.Scheme {
		return ErrHTUMismatch
	}
	if normalizePath(claimedURL) != normalizePath(actualURL) {
		return ErrHTUMismatch
	}

	return nil
}

// normalizePath returns a canonical form of the URL path for comparison.
// It cleans the path (resolving . and .., removing duplicate slashes),
// ensures a leading slash for empty paths, and decodes percent-encoding.
func normalizePath(u *url.URL) string {
	p := path.Clean(u.Path)
	if p == "." || p == "" {
		p = "/"
	}
	decoded, err := url.PathUnescape(p)
	if err != nil {
		return p
	}
	return decoded
}
