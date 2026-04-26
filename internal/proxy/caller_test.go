package proxy_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fambr/arx/internal/proxy"
)

// stubResolver implements proxy.ToolDataResolver for tests.
type stubResolver struct {
	data map[string]*proxy.ToolData
}

func (s *stubResolver) ResolveToolData(_ context.Context, tenantID, toolName string) (*proxy.ToolData, error) {
	key := tenantID + ":" + toolName
	td, ok := s.data[key]
	if !ok {
		return nil, proxy.ErrToolNotFound
	}
	return td, nil
}

func TestCaller_SuccessfulPOST(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["item_id"] != "SKU-42" {
			t.Errorf("expected item_id=SKU-42, got %v", got["item_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "added"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:add-cart": {
			UpstreamURL:    srv.URL + "/cart/add",
			UpstreamMethod: "POST",
			ParamMapping:   `{"product_id":"body.item_id","quantity":"body.qty"}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)
	result, err := caller.CallTool(
		context.Background(),
		"t1", "add-cart",
		map[string]any{"product_id": "SKU-42", "quantity": 3},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, ok := result.(*proxy.UpstreamResponse)
	if !ok {
		t.Fatalf("expected *UpstreamResponse, got %T", result)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", resp.Body)
	}
	if m["status"] != "added" {
		t.Errorf("expected status=added, got %v", m["status"])
	}
}

func TestCaller_SuccessfulGET(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Query().Get("cat") != "electronics" {
			t.Errorf("expected query cat=electronics, got %s", r.URL.Query().Get("cat"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []string{"phone", "laptop"}})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:search": {
			UpstreamURL:    srv.URL + "/products/search",
			UpstreamMethod: "GET",
			ParamMapping:   `{"category":"query.cat"}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)
	result, err := caller.CallTool(
		context.Background(),
		"t1", "search",
		map[string]any{"category": "electronics"},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, ok := result.(*proxy.UpstreamResponse)
	if !ok {
		t.Fatalf("expected *UpstreamResponse, got %T", result)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", resp.Body)
	}
	if m["results"] == nil {
		t.Error("expected results field")
	}
}

func TestCaller_ToolNotFound(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{}}
	caller := proxy.NewCaller(resolver)

	_, err := caller.CallTool(
		context.Background(),
		"t1", "nonexistent",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)

	if err != proxy.ErrToolNotFound {
		t.Fatalf("expected ErrToolNotFound, got %v", err)
	}
}

func TestCaller_PerToolTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Sleep longer than the tool's timeout.
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:slow-tool": {
			UpstreamURL:    srv.URL + "/slow",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      50, // 50ms — upstream sleeps 200ms
		},
	}}

	caller := proxy.NewCaller(resolver)
	_, err := caller.CallTool(
		context.Background(),
		"t1", "slow-tool",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "upstream_timeout") {
		t.Fatalf("expected upstream_timeout error, got: %v", err)
	}
}

func TestCaller_CircuitBreakerOpens(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:failing": {
			UpstreamURL:    srv.URL + "/fail",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver, proxy.WithCBThreshold(3))

	// Trigger 3 failures to open the breaker.
	for i := 0; i < 3; i++ {
		_, err := caller.CallTool(
			context.Background(),
			"t1", "failing",
			map[string]any{},
			httptest.NewRequest(http.MethodPost, "/mcp", nil),
		)
		if err == nil {
			t.Fatalf("expected error on call %d", i+1)
		}
	}

	// Next call should be rejected by the circuit breaker without hitting upstream.
	callsBefore := calls.Load()
	_, err := caller.CallTool(
		context.Background(),
		"t1", "failing",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)

	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
	if !strings.Contains(err.Error(), "circuit_open") {
		t.Fatalf("expected circuit_open error, got: %v", err)
	}
	if calls.Load() != callsBefore {
		t.Error("circuit breaker should not have made an upstream call")
	}
}

func TestCaller_CircuitBreakerHalfOpen(t *testing.T) {
	t.Parallel()

	var shouldFail atomic.Bool
	shouldFail.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if shouldFail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:flaky": {
			UpstreamURL:    srv.URL + "/flaky",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver,
		proxy.WithCBThreshold(2),
		proxy.WithCBResetTimeout(50*time.Millisecond),
	)

	// Open the breaker.
	for i := 0; i < 2; i++ {
		_, _ = caller.CallTool(
			context.Background(),
			"t1", "flaky",
			map[string]any{},
			httptest.NewRequest(http.MethodPost, "/mcp", nil),
		)
	}

	// Wait for reset timeout to transition to half-open.
	time.Sleep(80 * time.Millisecond)

	// Fix the upstream.
	shouldFail.Store(false)

	// Probe should succeed and close the breaker.
	result, err := caller.CallTool(
		context.Background(),
		"t1", "flaky",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("expected success on half-open probe, got: %v", err)
	}

	resp, ok := result.(*proxy.UpstreamResponse)
	if !ok {
		t.Fatalf("expected *UpstreamResponse, got %T", result)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", resp.Body)
	}
	if m["ok"] != "true" {
		t.Errorf("expected ok=true, got %v", m["ok"])
	}
}

func TestCaller_Upstream5xxMasked(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"internal":"secret details"}`))
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:broken": {
			UpstreamURL:    srv.URL + "/broken",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)
	_, err := caller.CallTool(
		context.Background(),
		"t1", "broken",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)

	if err == nil {
		t.Fatal("expected error on 5xx")
	}
	if !strings.Contains(err.Error(), "upstream_error") {
		t.Fatalf("expected upstream_error, got: %v", err)
	}
	// Internal details must not leak.
	if strings.Contains(err.Error(), "secret details") {
		t.Error("upstream body should not leak in error")
	}
}

func TestCaller_Upstream4xxForwarded(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid product"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:validate": {
			UpstreamURL:    srv.URL + "/validate",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)
	result, err := caller.CallTool(
		context.Background(),
		"t1", "validate",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("4xx should not be an error, got: %v", err)
	}

	resp, ok := result.(*proxy.UpstreamResponse)
	if !ok {
		t.Fatalf("expected *UpstreamResponse, got %T", result)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", resp.StatusCode)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", resp.Body)
	}
	if m["error"] != "invalid product" {
		t.Errorf("expected forwarded error, got %v", m["error"])
	}
}

func TestCaller_UpstreamNonJSONResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain text response"))
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:text-tool": {
			UpstreamURL:    srv.URL + "/text",
			UpstreamMethod: "GET",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)
	result, err := caller.CallTool(
		context.Background(),
		"t1", "text-tool",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, ok := result.(*proxy.UpstreamResponse)
	if !ok {
		t.Fatalf("expected *UpstreamResponse, got %T", result)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	text, ok := resp.Body.(string)
	if !ok {
		t.Fatalf("expected string body, got %T", resp.Body)
	}
	if text != "plain text response" {
		t.Errorf("expected plain text response, got %q", text)
	}
}

func TestCaller_Upstream4xxPreservesStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"bad_request", http.StatusBadRequest},
		{"not_found", http.StatusNotFound},
		{"conflict", http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": tt.name})
			}))
			defer srv.Close()

			resolver := &stubResolver{data: map[string]*proxy.ToolData{
				"t1:tool": {
					UpstreamURL:    srv.URL + "/endpoint",
					UpstreamMethod: "POST",
					ParamMapping:   `{}`,
					TimeoutMs:      5000,
				},
			}}

			caller := proxy.NewCaller(resolver)
			result, err := caller.CallTool(
				context.Background(),
				"t1", "tool",
				map[string]any{},
				httptest.NewRequest(http.MethodPost, "/mcp", nil),
			)
			if err != nil {
				t.Fatalf("4xx should not be an error, got: %v", err)
			}

			resp, ok := result.(*proxy.UpstreamResponse)
			if !ok {
				t.Fatalf("expected *UpstreamResponse, got %T", result)
			}
			if resp.StatusCode != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, resp.StatusCode)
			}
		})
	}
}

func TestCaller_InvalidParamMapping(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:bad-map": {
			UpstreamURL:    "http://localhost/nope",
			UpstreamMethod: "POST",
			ParamMapping:   `not valid json`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)
	_, err := caller.CallTool(
		context.Background(),
		"t1", "bad-map",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)

	if err == nil {
		t.Fatal("expected error on bad param mapping")
	}
}

func TestCaller_MixedBodyAndQuery(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Check query param.
		if r.URL.Query().Get("page") != "2" {
			t.Errorf("expected query page=2, got %s", r.URL.Query().Get("page"))
		}
		// Check body param.
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["search_term"] != "laptop" {
			t.Errorf("expected search_term=laptop, got %v", got["search_term"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:search": {
			UpstreamURL:    srv.URL + "/search",
			UpstreamMethod: "POST",
			ParamMapping:   `{"query":"body.search_term","page":"query.page"}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver)
	result, err := caller.CallTool(
		context.Background(),
		"t1", "search",
		map[string]any{"query": "laptop", "page": "2"},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, ok := result.(*proxy.UpstreamResponse)
	if !ok {
		t.Fatalf("expected *UpstreamResponse, got %T", result)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", resp.Body)
	}
	if m["ok"] != "true" {
		t.Errorf("expected ok=true, got %v", m["ok"])
	}
}

func TestCaller_CircuitBreakerPerTool(t *testing.T) {
	t.Parallel()

	var failCalls atomic.Int32
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer okSrv.Close()

	resolver := &stubResolver{data: map[string]*proxy.ToolData{
		"t1:failing": {
			UpstreamURL:    failSrv.URL + "/fail",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
		"t1:working": {
			UpstreamURL:    okSrv.URL + "/ok",
			UpstreamMethod: "POST",
			ParamMapping:   `{}`,
			TimeoutMs:      5000,
		},
	}}

	caller := proxy.NewCaller(resolver, proxy.WithCBThreshold(2))

	// Open circuit for "failing" tool.
	for i := 0; i < 2; i++ {
		_, _ = caller.CallTool(
			context.Background(),
			"t1", "failing",
			map[string]any{},
			httptest.NewRequest(http.MethodPost, "/mcp", nil),
		)
	}

	// "working" tool should still work — independent circuit breaker.
	result, err := caller.CallTool(
		context.Background(),
		"t1", "working",
		map[string]any{},
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	)
	if err != nil {
		t.Fatalf("working tool should succeed, got: %v", err)
	}

	resp, ok := result.(*proxy.UpstreamResponse)
	if !ok {
		t.Fatalf("expected *UpstreamResponse, got %T", result)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", resp.Body)
	}
	if m["ok"] != "true" {
		t.Errorf("expected ok=true, got %v", m["ok"])
	}
}
