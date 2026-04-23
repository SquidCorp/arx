## ADDED Requirements

### Requirement: Parameter mapping and proxying
The system SHALL map standard tool catalog parameters to merchant-specific field paths (as defined in the tool's `param_mapping` configuration) and forward the transformed request to the tool's `upstream_url` using the configured HTTP method.

#### Scenario: Successful parameter mapping
- **WHEN** `call_tool` is invoked for `cart.add` with params `{ productId: "SKU-42", quantity: 3 }` and the tool's param_mapping is `{ productId: "body.item_id", quantity: "body.qty" }`
- **THEN** the system SHALL send `POST https://api.merchant.com/cart/add` with body `{ "item_id": "SKU-42", "qty": 3 }`

#### Scenario: Unmapped parameters are dropped
- **WHEN** the agent sends parameters not defined in the tool's catalog schema
- **THEN** the system SHALL drop unknown parameters and not forward them to the merchant

### Requirement: Ed25519 request signing on outbound proxy
The system SHALL sign every outbound proxied request with the tenant's AgentGate signing key. The signature SHALL cover the HTTP method, path, body, and timestamp. The signature and timestamp SHALL be sent as `X-AgentGate-Signature` and `X-AgentGate-Timestamp` headers.

#### Scenario: Merchant can verify proxied request
- **WHEN** AgentGate proxies a request to the merchant upstream
- **THEN** the request SHALL include `X-AgentGate-Signature` and `X-AgentGate-Timestamp` headers, and the signature SHALL be verifiable with AgentGate's public key for that tenant

### Requirement: Session context headers on proxied requests
The system SHALL include session context headers on every proxied request: `X-AgentGate-Session` (session ID), `X-AgentGate-User` (user ID or "anonymous"), `X-AgentGate-Scopes` (comma-separated scopes).

#### Scenario: Merchant receives session context
- **WHEN** AgentGate proxies a tool call for user "user-123" with scopes `["cart:read", "cart:write"]`
- **THEN** the proxied request SHALL include headers `X-AgentGate-Session: <session_id>`, `X-AgentGate-User: user-123`, `X-AgentGate-Scopes: cart:read,cart:write`

### Requirement: Upstream timeout and error handling
The system SHALL enforce a configurable per-tool timeout on upstream requests. If the upstream times out or returns a 5xx error, the system SHALL return a structured error to the agent without exposing internal upstream details.

#### Scenario: Upstream timeout
- **WHEN** the merchant upstream does not respond within the configured timeout
- **THEN** the system SHALL return an MCP error with `"upstream_timeout"`

#### Scenario: Upstream 5xx error
- **WHEN** the merchant upstream returns a 500-599 status code
- **THEN** the system SHALL return an MCP error with `"upstream_error"` without forwarding the upstream response body

#### Scenario: Upstream 4xx error
- **WHEN** the merchant upstream returns a 400-499 status code
- **THEN** the system SHALL forward the response status and body to the agent (merchant-level validation errors are the agent's concern)

### Requirement: Upstream response forwarding
The system SHALL forward successful upstream responses (2xx) as the MCP tool call result. The response body SHALL be returned as-is (JSON).

#### Scenario: Successful upstream response
- **WHEN** the merchant upstream returns 200 with a JSON body
- **THEN** the system SHALL return the JSON body as the `call_tool` result
