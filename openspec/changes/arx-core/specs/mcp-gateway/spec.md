## ADDED Requirements

### Requirement: MCP server over Streamable HTTP
The system SHALL expose an MCP server at `/mcp` using the Streamable HTTP transport (POST + SSE). The server SHALL support the standard MCP `initialize` handshake and `tools/list` protocol method.

#### Scenario: MCP initialize
- **WHEN** an agent sends an MCP `initialize` request
- **THEN** the system SHALL respond with server capabilities including `tools` support

#### Scenario: MCP tools/list returns static meta-tools
- **WHEN** an agent sends an MCP `tools/list` request
- **THEN** the system SHALL return exactly 4 tools: `list_tools`, `get_tool`, `call_tool`, `session_info`

### Requirement: list_tools — scope-filtered tool discovery
The `list_tools` tool SHALL return the list of merchant tools available for the current session, filtered by the session's scopes. Each entry SHALL include `name` and `description`. A valid token MUST be present.

#### Scenario: Agent discovers available tools
- **WHEN** an agent calls `list_tools` with a valid token whose scopes include `cart:read` and `cart:write`
- **THEN** the system SHALL return only tools whose `required_scopes` are a subset of the session's scopes

#### Scenario: No matching tools
- **WHEN** an agent calls `list_tools` but no registered tools match the session's scopes
- **THEN** the system SHALL return an empty list

#### Scenario: No valid token
- **WHEN** an agent calls `list_tools` without a valid token
- **THEN** the system SHALL reject with an MCP error indicating authentication required

### Requirement: get_tool — tool schema retrieval
The `get_tool` tool SHALL accept a `name` parameter and return the full schema for that tool, including parameter definitions and required scopes. A valid token MUST be present.

#### Scenario: Successful schema retrieval
- **WHEN** an agent calls `get_tool` with `name: "cart.add"` and the session has sufficient scopes
- **THEN** the system SHALL return the tool's name, description, parameter schema, and required scopes

#### Scenario: Tool not found or not accessible
- **WHEN** an agent calls `get_tool` with a tool name that doesn't exist or the session lacks required scopes
- **THEN** the system SHALL return an MCP error with `"unknown_tool"`

### Requirement: call_tool — authenticated tool execution
The `call_tool` tool SHALL accept `name` and `params` parameters. It SHALL require a valid DPoP-bound token, run the full enforcement pipeline (DPoP validation, scope check, constraint check), and proxy the request to the merchant's upstream endpoint.

#### Scenario: Successful tool execution
- **WHEN** an agent calls `call_tool` with valid token, DPoP proof, sufficient scopes, and valid params
- **THEN** the system SHALL proxy the request to the merchant upstream and return the response

#### Scenario: Missing DPoP proof
- **WHEN** an agent calls `call_tool` without a DPoP proof header
- **THEN** the system SHALL reject with `"invalid_dpop_proof"`

### Requirement: session_info — session metadata retrieval
The `session_info` tool SHALL return the current session's metadata including scopes, claims, `expires_at`, and status. A valid token MUST be present.

#### Scenario: Agent checks session info
- **WHEN** an agent calls `session_info` with a valid token
- **THEN** the system SHALL return `{ scopes, claims, expires_at, status }`
