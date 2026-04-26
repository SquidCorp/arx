package token

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// generateDPoPProof creates a valid DPoP proof JWT for testing.
func generateDPoPProof(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey, claims DPoPClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["typ"] = "dpop+jwt"
	token.Header["jwk"] = map[string]interface{}{
		"kty": "OKP",
		"crv": "Ed25519",
		"x":   base64.RawURLEncoding.EncodeToString(pub),
	}

	s, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign dpop proof: %v", err)
	}
	return s
}

func TestValidateDPoPProof_Valid(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	accessToken := "test-access-token"
	hash := sha256.Sum256([]byte(accessToken))
	ath := base64.RawURLEncoding.EncodeToString(hash[:])

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-1",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
		ATH: ath,
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	thumbprint, err := ValidateDPoPProof(proof, accessToken, "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("expected valid proof, got: %v", err)
	}
	if thumbprint == "" {
		t.Error("expected non-empty thumbprint")
	}
}

func TestValidateDPoPProof_InvalidSignature(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Different keypair for the JWK in header.
	pub2, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-2",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}

	// Sign with priv, but header claims pub2 — signature won't match.
	proof := generateDPoPProof(t, priv, pub2, claims)

	_, err = ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestValidateDPoPProof_StaleProof(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-3",
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	_, err = ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error for stale proof")
	}
}

func TestValidateDPoPProof_ReplayNonce(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-4",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	_, err = ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return false, nil // nonce already seen
	})
	if err == nil {
		t.Fatal("expected error for replay nonce")
	}
}

func TestValidateDPoPProof_ATHMismatch(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-5",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
		ATH: "wrong-ath-value",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	_, err = ValidateDPoPProof(proof, "different-token", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error for ATH mismatch")
	}
}

func TestValidateDPoPProof_HTMMismatch(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-6",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "GET",
		HTU: "https://arx.example.com/mcp",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	_, err = ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error for HTM mismatch")
	}
}

func TestValidateDPoPProof_HTUMismatch(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-7",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/other",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	_, err = ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error for HTU mismatch")
	}
}

func TestValidateDPoPProof_MissingJTI(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	_, err = ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error for missing jti")
	}
}

func TestValidateDPoPProof_MissingIAT(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID: "jti-8",
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	_, err = ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error for missing iat")
	}
}

func TestValidateDPoPProof_ThumbprintConsistency(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-9",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}

	proof := generateDPoPProof(t, priv, pub, claims)

	thumbprint1, err := ValidateDPoPProof(proof, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("first validation failed: %v", err)
	}

	// Same key should produce the same thumbprint.
	proof2 := generateDPoPProof(t, priv, pub, claims)
	thumbprint2, err := ValidateDPoPProof(proof2, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("second validation failed: %v", err)
	}

	if thumbprint1 != thumbprint2 {
		t.Errorf("thumbprints should be identical for same key: %q vs %q", thumbprint1, thumbprint2)
	}
}

func TestValidateDPoPProof_DifferentKeysDifferentThumbprints(t *testing.T) {
	pub1, priv1, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub2, priv2, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	claims1 := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-a",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}
	claims2 := DPoPClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-b",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		HTM: "POST",
		HTU: "https://arx.example.com/mcp",
	}

	proof1 := generateDPoPProof(t, priv1, pub1, claims1)
	proof2 := generateDPoPProof(t, priv2, pub2, claims2)

	thumbprint1, err := ValidateDPoPProof(proof1, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("first validation failed: %v", err)
	}

	thumbprint2, err := ValidateDPoPProof(proof2, "", "POST", "https://arx.example.com/mcp", func(_ string) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("second validation failed: %v", err)
	}

	if thumbprint1 == thumbprint2 {
		t.Error("different keys should produce different thumbprints")
	}
}

func TestMatchHTU(t *testing.T) {
	tests := []struct {
		claimed string
		actual  string
		wantErr bool
	}{
		{"https://arx.example.com/mcp", "https://arx.example.com/mcp", false},
		{"https://arx.example.com/mcp", "https://arx.example.com/mcp#fragment", false},
		{"https://arx.example.com/mcp", "https://ARX.example.com/mcp", false},
		{"https://arx.example.com/mcp", "http://arx.example.com/mcp", true},
		{"https://arx.example.com/mcp", "https://arx.example.com/other", true},
		{"https://arx.example.com:8080/mcp", "https://arx.example.com/mcp", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_vs_%s", tt.claimed, tt.actual), func(t *testing.T) {
			err := matchHTU(tt.claimed, tt.actual)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRequestURL_HTTP(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("POST", "http://arx.example.com/mcp", nil)
	got := RequestURL(req)
	if got != "http://arx.example.com/mcp" {
		t.Errorf("got %q, want http://arx.example.com/mcp", got)
	}
}

func TestRequestURL_HTTPS(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("POST", "https://arx.example.com/mcp", nil)
	req.TLS = &tls.ConnectionState{}
	got := RequestURL(req)
	if got != "https://arx.example.com/mcp" {
		t.Errorf("got %q, want https://arx.example.com/mcp", got)
	}
}

func TestNormalizePath_Empty(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("https://example.com")
	u.Path = ""
	got := normalizePath(u)
	if got != "/" {
		t.Errorf("got %q, want /", got)
	}
}

func TestNormalizePath_Dot(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("https://example.com/.")
	got := normalizePath(u)
	if got != "/" {
		t.Errorf("got %q, want /", got)
	}
}
