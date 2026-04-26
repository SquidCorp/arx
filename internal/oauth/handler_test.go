package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fambr/arx/internal/token"
)

func TestMetadata(t *testing.T) {
	issuerURL := "http://localhost:8080"
	issuer := token.NewIssuer(token.Config{
		Issuer:   issuerURL,
		Audience: "arx",
	}, nil)

	h := NewHandler(nil, nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()

	h.Metadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var meta map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&meta); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	tests := []struct {
		key  string
		want any
	}{
		{"issuer", issuerURL},
		{"authorization_endpoint", issuerURL + "/oauth/authorize"},
		{"token_endpoint", issuerURL + "/oauth/token"},
	}

	for _, tt := range tests {
		if got := meta[tt.key]; got != tt.want {
			t.Errorf("meta[%q] = %v, want %v", tt.key, got, tt.want)
		}
	}

	// Check array fields.
	checkStringArray := func(key string, want []string) {
		t.Helper()
		raw, ok := meta[key]
		if !ok {
			t.Errorf("missing key %q", key)
			return
		}
		arr, ok := raw.([]any)
		if !ok {
			t.Errorf("key %q not an array", key)
			return
		}
		if len(arr) != len(want) {
			t.Errorf("key %q has %d elements, want %d", key, len(arr), len(want))
			return
		}
		for i, v := range arr {
			if s, ok := v.(string); !ok || s != want[i] {
				t.Errorf("key %q[%d] = %v, want %v", key, i, v, want[i])
			}
		}
	}

	checkStringArray("response_types_supported", []string{"code"})
	checkStringArray("grant_types_supported", []string{"authorization_code", "refresh_token"})
	checkStringArray("code_challenge_methods_supported", []string{"S256"})
	checkStringArray("dpop_signing_alg_values_supported", []string{"EdDSA"})
	checkStringArray("token_endpoint_auth_methods_supported", []string{"none"})
}
