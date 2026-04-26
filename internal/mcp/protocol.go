// Package mcp implements the MCP (Model Context Protocol) server over
// Streamable HTTP transport. It exposes four static meta-tools (list_tools,
// get_tool, call_tool, session_info) that provide scope-filtered access to
// tenant tool catalogs.
package mcp

import "encoding/json"

// JSON-RPC 2.0 constants.
const jsonRPCVersion = "2.0"

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// MCP-specific error codes (application-defined range).
const (
	CodeAuthRequired = -32001
	CodeUnknownTool  = -32002
	CodeScopeError   = -32003
	CodeDPoPError    = -32004
)

// InitializeParams holds the client parameters for the initialize request.
type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    any        `json:"capabilities"`
	ClientInfo      ClientInfo `json:"clientInfo"`
}

// ClientInfo describes the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the server's response to an initialize request.
type InitializeResult struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    ServerCapability `json:"capabilities"`
	ServerInfo      ServerInfo       `json:"serverInfo"`
}

// ServerCapability describes what the server supports.
type ServerCapability struct {
	Tools *ToolCapability `json:"tools,omitempty"`
}

// ToolCapability signals the server supports tools.
type ToolCapability struct {
	ListChanged bool `json:"listChanged"`
}

// ServerInfo describes the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolDefinition describes a tool in the MCP tools/list response.
type ToolDefinition struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

// JSONSchema is a minimal JSON Schema representation for tool input parameters.
type JSONSchema struct {
	Type       string                `json:"type"`
	Properties map[string]SchemaItem `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
}

// SchemaItem describes a single property in a JSON Schema.
type SchemaItem struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolsListResult is the server's response to tools/list.
type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

// CallToolParams holds the parameters for a tools/call request.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolResult is the result of a tools/call invocation.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a typed content item in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
