package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fambr/arx/internal/token"
)

func TestAuthorize_Success(t *testing.T) {
	flowStore := newMockFlowStore()
	flowStore.tenants["tenant-1"] = &TenantOAuthConfig{
		ID:               "tenant-1",
		RedirectURIs:     []string{"http://llm.example.com/callback"},
		MerchantLoginURL: "https://merchant.example.com/login",
	}

	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(flowStore, nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/oauth/authorize?client_id=tenant-1&redirect_uri=http://llm.example.com/callback&state=abc123&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256",
		nil)
	rec := httptest.NewRecorder()

	h.Authorize(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}

	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("missing Location header")
	}

	// Should redirect to merchant login URL with agent_session_uuid param.
	if !contains(loc, "https://merchant.example.com/login") {
		t.Errorf("expected redirect to merchant login, got %s", loc)
	}
	if !contains(loc, "agent_session_uuid=") {
		t.Errorf("expected agent_session_uuid param, got %s", loc)
	}

	// Verify flow was stored.
	if len(flowStore.flows) != 1 {
		t.Fatalf("expected 1 flow stored, got %d", len(flowStore.flows))
	}

	for _, flow := range flowStore.flows {
		if flow.Status != "pending" {
			t.Errorf("expected status 'pending', got %q", flow.Status)
		}
		if flow.ClientID != "tenant-1" {
			t.Errorf("expected client_id 'tenant-1', got %q", flow.ClientID)
		}
		if flow.CodeChallenge != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
			t.Errorf("code challenge mismatch")
		}
	}
}

func TestAuthorize_InvalidRedirectURI(t *testing.T) {
	flowStore := newMockFlowStore()
	flowStore.tenants["tenant-1"] = &TenantOAuthConfig{
		ID:               "tenant-1",
		RedirectURIs:     []string{"http://allowed.example.com/callback"},
		MerchantLoginURL: "https://merchant.example.com/login",
	}

	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(flowStore, nil, nil, nil, nil, issuer, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/oauth/authorize?client_id=tenant-1&redirect_uri=http://evil.example.com/steal&state=abc&code_challenge=xxx&code_challenge_method=S256",
		nil)
	rec := httptest.NewRecorder()

	h.Authorize(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	assertErrorCode(t, rec, "invalid_redirect_uri")
}

func TestAuthorize_MissingParams(t *testing.T) {
	issuer := token.NewIssuer(token.Config{Issuer: "http://localhost:8080", Audience: "arx"}, nil)
	h := NewHandler(newMockFlowStore(), nil, nil, nil, nil, issuer, nil)

	tests := []struct {
		name string
		url  string
		want string
	}{
		{"missing client_id", "/oauth/authorize?redirect_uri=x&state=y&code_challenge=z&code_challenge_method=S256", "missing_client_id"},
		{"missing redirect_uri", "/oauth/authorize?client_id=x&state=y&code_challenge=z&code_challenge_method=S256", "missing_redirect_uri"},
		{"missing state", "/oauth/authorize?client_id=x&redirect_uri=y&code_challenge=z&code_challenge_method=S256", "missing_state"},
		{"missing code_challenge", "/oauth/authorize?client_id=x&redirect_uri=y&state=z&code_challenge_method=S256", "missing_code_challenge"},
		{"invalid method", "/oauth/authorize?client_id=x&redirect_uri=y&state=z&code_challenge=abc&code_challenge_method=plain", "invalid_code_challenge_method"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			rec := httptest.NewRecorder()
			h.Authorize(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", rec.Code)
			}
			assertErrorCode(t, rec, tt.want)
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var resp map[string]string
	if err := decodeJSON(rec, &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] != expected {
		t.Errorf("error = %q, want %q", resp["error"], expected)
	}
}

func decodeJSON(rec *httptest.ResponseRecorder, v any) error {
	return jsonDecode(rec.Body.Bytes(), v)
}

func jsonDecode(data []byte, v any) error {
	return jsonUnmarshal(data, v)
}

var jsonUnmarshal = func(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
