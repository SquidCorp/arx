package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fambr/arx/internal/proxy"
	"github.com/fambr/arx/internal/scope"
)

// ErrNoToken is returned when no valid access token is found on the request.
var ErrNoToken = errors.New("authentication_required")

// TokenInfo holds the validated session data extracted from an access token.
type TokenInfo struct {
	SessionID string
	TenantID  string
	UserID    string
	Scopes    []string
	ExpiresAt time.Time
	Status    string
	Claims    map[string]any
}

// TokenExtractor validates the access token on a request and returns session info.
type TokenExtractor interface {
	ExtractToken(r *http.Request) (*TokenInfo, error)
}

// ToolProvider retrieves the registered tools for a tenant.
type ToolProvider interface {
	TenantTools(ctx context.Context, tenantID string) ([]scope.Tool, error)
}

// ToolCaller proxies a tool call to the merchant upstream.
type ToolCaller interface {
	CallTool(ctx context.Context, tenantID, toolName string, params map[string]any, r *http.Request) (any, error)
}

// Handler implements the MCP Streamable HTTP transport.
type Handler struct {
	tokenExtractor TokenExtractor
	toolProvider   ToolProvider
	toolCaller     ToolCaller
}

// NewHandler creates an MCP handler with the given dependencies.
func NewHandler(tokenExtractor TokenExtractor, toolProvider ToolProvider, toolCaller ToolCaller) *Handler {
	return &Handler{
		tokenExtractor: tokenExtractor,
		toolProvider:   toolProvider,
		toolCaller:     toolCaller,
	}
}

// ServeHTTP handles MCP Streamable HTTP requests (POST only).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPC(w, Response{
			JSONRPC: jsonRPCVersion,
			ID:      nil,
			Error:   &RPCError{Code: CodeParseError, Message: "parse error"},
		})
		return
	}

	if req.JSONRPC != jsonRPCVersion {
		writeJSONRPC(w, Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidRequest, Message: "invalid jsonrpc version"},
		})
		return
	}

	// Handle notifications (id is null or absent).
	if isNotification(req) {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := h.dispatch(r, &req)
	writeJSONRPC(w, resp)
}

// dispatch routes a JSON-RPC request to the appropriate handler.
func (h *Handler) dispatch(r *http.Request, req *Request) Response {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(req)
	case "tools/list":
		return h.handleToolsList(req)
	case "tools/call":
		return h.handleToolsCall(r, req)
	default:
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeMethodNotFound, Message: "method not found"},
		}
	}
}

// handleInitialize responds to the MCP initialize handshake.
func (h *Handler) handleInitialize(req *Request) Response {
	result := InitializeResult{
		ProtocolVersion: "2025-03-26",
		Capabilities: ServerCapability{
			Tools: &ToolCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo{
			Name:    "arx",
			Version: "1.0.0",
		},
	}
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result:  result,
	}
}

// handleToolsList returns the 4 static meta-tools.
func (h *Handler) handleToolsList(req *Request) Response {
	tools := metaToolDefinitions()
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Result:  ToolsListResult{Tools: tools},
	}
}

// handleToolsCall dispatches to the named meta-tool.
func (h *Handler) handleToolsCall(r *http.Request, req *Request) Response {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "invalid params"},
		}
	}

	switch params.Name {
	case "list_tools":
		return h.execListTools(r, req)
	case "get_tool":
		return h.execGetTool(r, req, params)
	case "call_tool":
		return h.execCallTool(r, req, params)
	case "session_info":
		return h.execSessionInfo(r, req)
	default:
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeMethodNotFound, Message: "unknown tool: " + params.Name},
		}
	}
}

// execListTools returns scope-filtered tenant tools.
func (h *Handler) execListTools(r *http.Request, req *Request) Response {
	info, err := h.requireToken(r)
	if err != nil {
		return authError(req.ID)
	}

	tools, err := h.toolProvider.TenantTools(r.Context(), info.TenantID)
	if err != nil {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInternalError, Message: "internal error"},
		}
	}

	filtered := scope.FilterTools(tools, info.Scopes)
	type toolEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	entries := make([]toolEntry, 0, len(filtered))
	for _, t := range filtered {
		entry, ok := scope.GetCatalogEntry(t.CatalogType)
		desc := t.CatalogType
		if ok {
			desc = entry.Description
		}
		entries = append(entries, toolEntry{Name: t.Name, Description: desc})
	}

	text, _ := json.Marshal(entries)
	return toolResult(req.ID, string(text), false)
}

// execGetTool returns the full schema for a tool if accessible.
func (h *Handler) execGetTool(r *http.Request, req *Request, params CallToolParams) Response {
	info, err := h.requireToken(r)
	if err != nil {
		return authError(req.ID)
	}

	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params.Arguments, &args); err != nil || args.Name == "" {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "missing name parameter"},
		}
	}

	tools, err := h.toolProvider.TenantTools(r.Context(), info.TenantID)
	if err != nil {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInternalError, Message: "internal error"},
		}
	}

	// Find the tool and check scope access.
	filtered := scope.FilterTools(tools, info.Scopes)
	for _, t := range filtered {
		if t.Name == args.Name {
			entry, ok := scope.GetCatalogEntry(t.CatalogType)
			if !ok {
				break
			}
			result := map[string]any{
				"name":            t.Name,
				"catalogType":     t.CatalogType,
				"description":     entry.Description,
				"requiredScopes":  entry.RequiredScopes,
				"parameterSchema": entry.Params,
			}
			text, _ := json.Marshal(result)
			return toolResult(req.ID, string(text), false)
		}
	}

	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      req.ID,
		Error:   &RPCError{Code: CodeUnknownTool, Message: "unknown_tool"},
	}
}

// execCallTool proxies a tool call through the enforcement pipeline.
func (h *Handler) execCallTool(r *http.Request, req *Request, params CallToolParams) Response {
	info, err := h.requireToken(r)
	if err != nil {
		return authError(req.ID)
	}

	var args struct {
		Name   string         `json:"name"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(params.Arguments, &args); err != nil || args.Name == "" {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "missing name parameter"},
		}
	}

	// Verify the DPoP proof is present on the HTTP request.
	if r.Header.Get("DPoP") == "" {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeDPoPError, Message: "invalid_dpop_proof"},
		}
	}

	// Check tool exists and scopes match.
	tools, err := h.toolProvider.TenantTools(r.Context(), info.TenantID)
	if err != nil {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInternalError, Message: "internal error"},
		}
	}

	var found *scope.Tool
	for i, t := range tools {
		if t.Name == args.Name {
			found = &tools[i]
			break
		}
	}
	if found == nil {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeUnknownTool, Message: "unknown_tool"},
		}
	}

	if !scope.MatchScopes(info.Scopes, found.RequiredScopes) {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeScopeError, Message: "insufficient_scope"},
		}
	}

	// Evaluate constraints from session scopes.
	entry, ok := scope.GetCatalogEntry(found.CatalogType)
	if !ok {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeUnknownTool, Message: "unknown_catalog_type"},
		}
	}

	// Parse constraints from session scopes that match this tool's required scopes.
	var constraints []scope.Constraint
	for _, reqScope := range entry.RequiredScopes {
		constraints = append(constraints, scope.FindConstraints(info.Scopes, reqScope)...)
	}

	if err := scope.EvaluateConstraints(constraints, args.Params); err != nil {
		return Response{
			JSONRPC: jsonRPCVersion,
			ID:      req.ID,
			Error:   &RPCError{Code: CodeScopeError, Message: err.Error()},
		}
	}

	// Proxy to upstream via ToolCaller.
	if h.toolCaller == nil {
		// No upstream configured — return a placeholder.
		text, _ := json.Marshal(map[string]string{"status": "proxied", "tool": args.Name})
		return toolResult(req.ID, string(text), false)
	}

	// Inject session context for upstream header injection.
	userID := info.UserID
	if userID == "" {
		userID = "anonymous"
	}
	ctx := proxy.WithSessionContext(r.Context(), &proxy.SessionContext{
		SessionID: info.SessionID,
		UserID:    userID,
		Scopes:    info.Scopes,
	})

	result, err := h.toolCaller.CallTool(ctx, info.TenantID, args.Name, args.Params, r)
	if err != nil {
		return h.upstreamErrorResponse(req.ID, err)
	}

	// If the caller returns an UpstreamResponse, use it to set isError for 4xx.
	if resp, ok := result.(*proxy.UpstreamResponse); ok {
		text, _ := json.Marshal(resp.Body)
		isError := resp.StatusCode >= 400 && resp.StatusCode < 500
		return toolResult(req.ID, string(text), isError)
	}

	text, _ := json.Marshal(result)
	return toolResult(req.ID, string(text), false)
}

// upstreamErrorResponse maps proxy sentinel errors to structured MCP error
// responses with specific error messages.
func (h *Handler) upstreamErrorResponse(id json.RawMessage, err error) Response {
	code := CodeInternalError
	msg := "upstream error"

	switch {
	case errors.Is(err, proxy.ErrUpstreamTimeout):
		code = CodeUpstreamError
		msg = "upstream_timeout"
	case errors.Is(err, proxy.ErrUpstreamError):
		code = CodeUpstreamError
		msg = "upstream_error"
	case errors.Is(err, proxy.ErrCircuitOpen):
		code = CodeUpstreamError
		msg = "circuit_open"
	}

	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	}
}

// execSessionInfo returns the current session's metadata.
func (h *Handler) execSessionInfo(r *http.Request, req *Request) Response {
	info, err := h.requireToken(r)
	if err != nil {
		return authError(req.ID)
	}

	result := map[string]any{
		"sessionId": info.SessionID,
		"scopes":    info.Scopes,
		"claims":    info.Claims,
		"expiresAt": info.ExpiresAt.Format(time.RFC3339),
		"status":    info.Status,
	}
	text, _ := json.Marshal(result)
	return toolResult(req.ID, string(text), false)
}

// requireToken extracts and validates the access token from the request.
func (h *Handler) requireToken(r *http.Request) (*TokenInfo, error) {
	if h.tokenExtractor == nil {
		return nil, ErrNoToken
	}
	return h.tokenExtractor.ExtractToken(r)
}

// authError returns a standard authentication required error response.
func authError(id json.RawMessage) Response {
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: CodeAuthRequired, Message: "authentication_required"},
	}
}

// toolResult wraps text content in an MCP tool result response.
func toolResult(id json.RawMessage, text string, isError bool) Response {
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result: ToolResult{
			Content: []ContentBlock{{Type: "text", Text: text}},
			IsError: isError,
		},
	}
}

// isNotification checks if the request is a JSON-RPC notification (null or absent id).
func isNotification(req Request) bool {
	return req.ID == nil || string(req.ID) == "null"
}

// writeJSONRPC writes a JSON-RPC response.
func writeJSONRPC(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
