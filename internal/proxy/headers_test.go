package proxy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fambr/arx/internal/proxy"
)

func TestSessionContext_RoundTrip(t *testing.T) {
	t.Parallel()

	sc := &proxy.SessionContext{
		SessionID: "sess-001",
		UserID:    "user-123",
		Scopes:    []string{"cart:read", "cart:write"},
	}

	ctx := proxy.WithSessionContext(context.Background(), sc)
	got := proxy.SessionContextFrom(ctx)

	if got == nil {
		t.Fatal("expected session context in context")
	}
	if got.SessionID != sc.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, sc.SessionID)
	}
	if got.UserID != sc.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, sc.UserID)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "cart:read" || got.Scopes[1] != "cart:write" {
		t.Errorf("Scopes = %v, want %v", got.Scopes, sc.Scopes)
	}
}

func TestSessionContextFrom_Missing(t *testing.T) {
	t.Parallel()

	got := proxy.SessionContextFrom(context.Background())
	if got != nil {
		t.Errorf("expected nil when no session context, got %+v", got)
	}
}

func TestSessionContext_AnonymousUser(t *testing.T) {
	t.Parallel()

	sc := &proxy.SessionContext{
		SessionID: "sess-002",
		UserID:    "anonymous",
		Scopes:    []string{"products:read"},
	}

	ctx := proxy.WithSessionContext(context.Background(), sc)
	got := proxy.SessionContextFrom(ctx)

	if got == nil {
		t.Fatal("expected session context in context")
	}
	if got.UserID != "anonymous" {
		t.Errorf("UserID = %q, want %q", got.UserID, "anonymous")
	}
}

func TestCaller_SessionContextHeaders(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:cart.add": {
			UpstreamURL:    srv.URL + "/cart/add",
			UpstreamMethod: "POST",
			ParamMapping:   `{"product_id":"body.item_id"}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)

	sc := &proxy.SessionContext{
		SessionID: "sess-abc",
		UserID:    "user-123",
		Scopes:    []string{"cart:read", "cart:write"},
	}
	ctx := proxy.WithSessionContext(context.Background(), sc)

	_, err := caller.CallTool(
		ctx,
		"t1", "cart.add",
		map[string]any{"product_id": "SKU-42"},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := receivedHeaders.Get("X-Arx-Session"); got != "sess-abc" {
		t.Errorf("X-Arx-Session = %q, want %q", got, "sess-abc")
	}
	if got := receivedHeaders.Get("X-Arx-User"); got != "user-123" {
		t.Errorf("X-Arx-User = %q, want %q", got, "user-123")
	}
	if got := receivedHeaders.Get("X-Arx-Scopes"); got != "cart:read,cart:write" {
		t.Errorf("X-Arx-Scopes = %q, want %q", got, "cart:read,cart:write")
	}
}

func TestCaller_SessionContextAnonymous(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:products.search": {
			UpstreamURL:    srv.URL + "/search",
			UpstreamMethod: "GET",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)

	sc := &proxy.SessionContext{
		SessionID: "sess-anon",
		UserID:    "anonymous",
		Scopes:    []string{"products:read"},
	}
	ctx := proxy.WithSessionContext(context.Background(), sc)

	_, err := caller.CallTool(
		ctx,
		"t1", "products.search",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := receivedHeaders.Get("X-Arx-User"); got != "anonymous" {
		t.Errorf("X-Arx-User = %q, want %q", got, "anonymous")
	}
}

func TestCaller_NoSessionContextNoHeaders(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:products.search": {
			UpstreamURL:    srv.URL + "/search",
			UpstreamMethod: "GET",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)

	// No session context in the context.
	_, err := caller.CallTool(
		context.Background(),
		"t1", "products.search",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := receivedHeaders.Get("X-Arx-Session"); got != "" {
		t.Errorf("X-Arx-Session should be empty without session context, got %q", got)
	}
	if got := receivedHeaders.Get("X-Arx-User"); got != "" {
		t.Errorf("X-Arx-User should be empty without session context, got %q", got)
	}
	if got := receivedHeaders.Get("X-Arx-Scopes"); got != "" {
		t.Errorf("X-Arx-Scopes should be empty without session context, got %q", got)
	}
}
