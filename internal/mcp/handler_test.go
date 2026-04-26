package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fambr/arx/internal/mcp"
	"github.com/fambr/arx/internal/proxy"
	"github.com/fambr/arx/internal/scope"
)

// jsonRPCRequest builds a JSON-RPC request body.
func jsonRPCRequest(t *testing.T, id int, method string, params any) []byte {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return b
}

// parseResponse parses a JSON-RPC response from the body.
func parseResponse(t *testing.T, body io.Reader) mcp.Response {
	t.Helper()
	var resp mcp.Response
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func newTestHandler() *mcp.Handler {
	return mcp.NewHandler(nil, nil, nil)
}

func TestHandler_RejectsNonPOST(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandler_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`not json`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeParseError {
		t.Errorf("expected parse error, got %+v", resp.Error)
	}
}

func TestHandler_RejectsInvalidJSONRPC(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := []byte(`{"jsonrpc":"1.0","id":1,"method":"initialize"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInvalidRequest {
		t.Errorf("expected invalid request error, got %+v", resp.Error)
	}
}

func TestHandler_Initialize(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test-client", "version": "1.0"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result mcp.InitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.ProtocolVersion != "2025-03-26" {
		t.Errorf("protocolVersion = %q, want %q", result.ProtocolVersion, "2025-03-26")
	}
	if result.Capabilities.Tools == nil {
		t.Fatal("expected tools capability to be present")
	}
	if result.ServerInfo.Name != "arx" {
		t.Errorf("serverInfo.name = %q, want %q", result.ServerInfo.Name, "arx")
	}
}

func TestHandler_UnknownMethod(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "nonexistent/method", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeMethodNotFound {
		t.Errorf("expected method not found error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsList(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "tools/list", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result mcp.ToolsListResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(result.Tools))
	}

	expected := map[string]bool{
		"list_tools":   false,
		"get_tool":     false,
		"call_tool":    false,
		"session_info": false,
	}
	for _, tool := range result.Tools {
		if _, ok := expected[tool.Name]; !ok {
			t.Errorf("unexpected tool %q", tool.Name)
		}
		expected[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want %q", tool.Name, tool.InputSchema.Type, "object")
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing expected tool %q", name)
		}
	}
}

func TestHandler_ToolsCall_ListTools_NoToken(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "list_tools",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeAuthRequired {
		t.Errorf("expected auth required error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_ListTools_WithToken(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
		{Name: "view-cart", CatalogType: "cart.view", RequiredScopes: []string{"cart:read"}},
	}
	toolProvider := &stubToolProvider{tools: tools}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"},
		},
		toolProvider,
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "list_tools",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result mcp.ToolResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Fatal("unexpected isError=true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}

	// Parse the tool list from the text content.
	var toolList []map[string]string
	if err := json.Unmarshal([]byte(result.Content[0].Text), &toolList); err != nil {
		t.Fatalf("unmarshal tool list: %v", err)
	}

	// Should only include view-cart (cart:read scope match).
	if len(toolList) != 1 {
		t.Fatalf("expected 1 filtered tool, got %d", len(toolList))
	}
	if toolList[0]["name"] != "view-cart" {
		t.Errorf("expected view-cart, got %q", toolList[0]["name"])
	}
}

func TestHandler_ToolsCall_GetTool_Found(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: tools},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "get_tool",
		"arguments": map[string]any{"name": "add-to-cart"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result mcp.ToolResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected isError=true")
	}

	// Parse the tool schema from text.
	var toolSchema map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &toolSchema); err != nil {
		t.Fatalf("unmarshal tool schema: %v", err)
	}
	if toolSchema["name"] != "add-to-cart" {
		t.Errorf("expected name add-to-cart, got %v", toolSchema["name"])
	}
}

func TestHandler_ToolsCall_GetTool_NotFound(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: nil},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "get_tool",
		"arguments": map[string]any{"name": "nonexistent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeUnknownTool {
		t.Errorf("expected unknown_tool error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_GetTool_InsufficientScope(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "checkout", CatalogType: "checkout.exec", RequiredScopes: []string{"checkout:exec"}},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"}, // no checkout:exec
		},
		&stubToolProvider{tools: tools},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "get_tool",
		"arguments": map[string]any{"name": "checkout"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeUnknownTool {
		t.Errorf("expected unknown_tool error for scope mismatch, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_SessionInfo(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-123",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read", "cart:write"},
			expiresAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			status:    "ACTIVE",
			claims:    map[string]any{"userId": "user-42"},
		},
		nil,
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "session_info",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result mcp.ToolResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &info); err != nil {
		t.Fatalf("unmarshal session info: %v", err)
	}

	if info["status"] != "ACTIVE" {
		t.Errorf("status = %v, want ACTIVE", info["status"])
	}
	scopes, ok := info["scopes"].([]any)
	if !ok || len(scopes) != 2 {
		t.Errorf("expected 2 scopes, got %v", info["scopes"])
	}
}

func TestHandler_ToolsCall_CallTool_NoDPoP(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: tools},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{"product_id": "abc", "quantity": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No DPoP header.
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeDPoPError {
		t.Errorf("expected DPoP error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_CallTool_InsufficientScope(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "checkout", CatalogType: "checkout.exec", RequiredScopes: []string{"checkout:exec"}},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"}, // no checkout:exec
		},
		&stubToolProvider{tools: tools},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "checkout",
			"params": map[string]any{"amount": 100, "currency": "USD"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeScopeError {
		t.Errorf("expected scope error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_CallTool_ConstraintViolation(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "checkout", CatalogType: "checkout.exec", RequiredScopes: []string{"checkout:exec"}},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"checkout:exec:maxAmount=50"}, // max 50
		},
		&stubToolProvider{tools: tools},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "checkout",
			"params": map[string]any{"amount": 100.0, "currency": "USD"}, // exceeds 50
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeScopeError {
		t.Errorf("expected constraint violation, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_CallTool_Success(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: tools},
		nil, // no ToolCaller — placeholder response
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{"product_id": "abc", "quantity": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result mcp.ToolResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.IsError {
		t.Error("unexpected isError=true")
	}
}

func TestHandler_ToolsCall_CallTool_UnknownTool(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: nil},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "nonexistent",
			"params": map[string]any{},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeUnknownTool {
		t.Errorf("expected unknown_tool error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_UnknownMetaTool(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"},
		},
		nil,
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeMethodNotFound {
		t.Errorf("expected method not found for unknown tool, got %+v", resp.Error)
	}
}

func TestHandler_NotificationsInitialized(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 0, "notifications/initialized", nil)
	// Notifications have null id — override.
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	raw["id"] = nil
	b, _ := json.Marshal(raw)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Notifications should return 202 Accepted with no body.
	if rec.Code != http.StatusAccepted {
		t.Errorf("got status %d, want 202", rec.Code)
	}
}

func TestHandler_ContentTypeJSON(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test", "version": "1.0"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestHandler_ToolsCall_CallTool_WithToolCaller(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
	}
	caller := &stubToolCaller{
		result: map[string]string{"status": "ok", "itemId": "123"},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: tools},
		caller,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{"product_id": "abc", "quantity": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_CallTool_ToolCallerError(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
	}
	caller := &stubToolCaller{err: fmt.Errorf("upstream timeout")}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: tools},
		caller,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{"product_id": "abc", "quantity": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInternalError {
		t.Errorf("expected internal error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_CallTool_NoToken(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeAuthRequired {
		t.Errorf("expected auth required, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_CallTool_ToolProviderError(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{err: fmt.Errorf("db error")},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInternalError {
		t.Errorf("expected internal error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_CallTool_InvalidParams(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:write"},
		},
		nil,
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "call_tool",
		"arguments": "not-an-object",
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInvalidParams {
		t.Errorf("expected invalid params, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_GetTool_NoToken(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "get_tool",
		"arguments": map[string]any{"name": "foo"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeAuthRequired {
		t.Errorf("expected auth required, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_SessionInfo_NoToken(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "session_info",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeAuthRequired {
		t.Errorf("expected auth required, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_ListTools_ProviderError(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"},
		},
		&stubToolProvider{err: fmt.Errorf("db error")},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "list_tools",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInternalError {
		t.Errorf("expected internal error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_GetTool_ProviderError(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"},
		},
		&stubToolProvider{err: fmt.Errorf("db error")},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "get_tool",
		"arguments": map[string]any{"name": "foo"},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInternalError {
		t.Errorf("expected internal error, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_GetTool_MissingNameParam(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"},
		},
		nil,
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "get_tool",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInvalidParams {
		t.Errorf("expected invalid params, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_InvalidToolsCallParams(t *testing.T) {
	t.Parallel()

	h := newTestHandler()
	body := jsonRPCRequest(t, 1, "tools/call", "not-an-object")
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error == nil || resp.Error.Code != mcp.CodeInvalidParams {
		t.Errorf("expected invalid params, got %+v", resp.Error)
	}
}

func TestHandler_ToolsCall_ListTools_EmptyResult(t *testing.T) {
	t.Parallel()

	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-1",
			tenantID:  "tenant-1",
			scopes:    []string{"cart:read"},
		},
		&stubToolProvider{tools: []scope.Tool{}},
		nil,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name":      "list_tools",
		"arguments": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result mcp.ToolResult
	_ = json.Unmarshal(resultBytes, &result)

	var toolList []map[string]string
	_ = json.Unmarshal([]byte(result.Content[0].Text), &toolList)
	if len(toolList) != 0 {
		t.Errorf("expected empty tool list, got %d", len(toolList))
	}
}

func TestHandler_ToolsCall_CallTool_SessionContextInjected(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
	}
	caller := &capturingToolCaller{
		result: map[string]string{"status": "ok"},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-ctx-1",
			tenantID:  "tenant-1",
			userID:    "user-42",
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: tools},
		caller,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{"product_id": "abc", "quantity": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	// Verify session context was injected into the context passed to CallTool.
	sc := caller.capturedSessionCtx
	if sc == nil {
		t.Fatal("expected session context in context passed to tool caller")
	}
	if sc.SessionID != "sess-ctx-1" {
		t.Errorf("SessionID = %q, want %q", sc.SessionID, "sess-ctx-1")
	}
	if sc.UserID != "user-42" {
		t.Errorf("UserID = %q, want %q", sc.UserID, "user-42")
	}
	if len(sc.Scopes) != 1 || sc.Scopes[0] != "cart:write" {
		t.Errorf("Scopes = %v, want [cart:write]", sc.Scopes)
	}
}

func TestHandler_ToolsCall_CallTool_AnonymousUserContext(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
	}
	caller := &capturingToolCaller{
		result: map[string]string{"status": "ok"},
	}
	h := mcp.NewHandler(
		&stubTokenExtractor{
			sessionID: "sess-anon",
			tenantID:  "tenant-1",
			userID:    "", // empty → should become "anonymous"
			scopes:    []string{"cart:write"},
		},
		&stubToolProvider{tools: tools},
		caller,
	)

	body := jsonRPCRequest(t, 1, "tools/call", map[string]any{
		"name": "call_tool",
		"arguments": map[string]any{
			"name":   "add-to-cart",
			"params": map[string]any{"product_id": "abc", "quantity": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DPoP", "dummy-proof")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	resp := parseResponse(t, rec.Body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	sc := caller.capturedSessionCtx
	if sc == nil {
		t.Fatal("expected session context in context passed to tool caller")
	}
	if sc.UserID != "anonymous" {
		t.Errorf("UserID = %q, want %q", sc.UserID, "anonymous")
	}
}

// --- Test stubs ---

// stubTokenExtractor implements mcp.TokenExtractor for testing.
type stubTokenExtractor struct {
	sessionID string
	tenantID  string
	userID    string
	scopes    []string
	expiresAt time.Time
	status    string
	claims    map[string]any
}

func (s *stubTokenExtractor) ExtractToken(_ *http.Request) (*mcp.TokenInfo, error) {
	if s == nil {
		return nil, mcp.ErrNoToken
	}
	return &mcp.TokenInfo{
		SessionID: s.sessionID,
		TenantID:  s.tenantID,
		UserID:    s.userID,
		Scopes:    s.scopes,
		ExpiresAt: s.expiresAt,
		Status:    s.status,
		Claims:    s.claims,
	}, nil
}

// stubToolProvider implements mcp.ToolProvider for testing.
type stubToolProvider struct {
	tools []scope.Tool
	err   error
}

func (s *stubToolProvider) TenantTools(_ context.Context, _ string) ([]scope.Tool, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tools, nil
}

// stubToolCaller implements mcp.ToolCaller for testing.
type stubToolCaller struct {
	result any
	err    error
}

func (s *stubToolCaller) CallTool(_ context.Context, _, _ string, _ map[string]any, _ *http.Request) (any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

// capturingToolCaller captures the session context from the context passed to CallTool.
type capturingToolCaller struct {
	result             any
	capturedSessionCtx *proxy.SessionContext
}

func (c *capturingToolCaller) CallTool(ctx context.Context, _, _ string, _ map[string]any, _ *http.Request) (any, error) {
	c.capturedSessionCtx = proxy.SessionContextFrom(ctx)
	return c.result, nil
}
