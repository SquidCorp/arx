## Why

AI agents (ChatGPT, Claude, Gemini, in-app assistants) need to call merchant backend tools (cart, checkout, orders) on behalf of authenticated users. Today, every merchant must build their own agent auth layer — handling token scoping, replay protection, and audit logging from scratch. AgentGate solves this by acting as a secure, vendor-agnostic authentication and authorization gateway that sits between agents and merchant backends, so that no agent ever reaches a merchant API directly.

## What Changes

- Introduce a **webhook-based session creation** system — the merchant's backend (after authenticating the user via its own auth system) fires `POST /webhook/session-connected` with a signed payload to create scoped, short-lived sessions
- Implement **DPoP-bound token issuance** — access tokens are cryptographically bound to the agent's keypair, preventing token theft and replay
- Expose an **MCP server (Streamable HTTP)** with 4 static meta-tools (`list_tools`, `get_tool`, `call_tool`, `session_info`) that provide tenant-specific, scope-filtered tool discovery and execution
- Build a **reverse proxy engine** that maps standardized tool catalog parameters to merchant-specific field names and forwards authorized requests to merchant upstream endpoints
- Implement **structured scope enforcement** with constraint checking (e.g., `checkout:exec:maxAmount=100:currency=EUR`) evaluated on every tool call before proxying
- Add **OAuth 2.1 + PKCE flow** for external LLM agents (Flow 1) with uuid-based correlation between the OAuth redirect and the merchant webhook
- Add **bind-code flow** for in-app agents (Flow 2) so DPoP keypairs stay in the agent's memory
- Implement **Ed25519 signature verification** on inbound webhooks and **Ed25519 signing** on outbound proxied requests (symmetric trust model)
- Build **immutable audit logging** for every tool call, session event, and enforcement decision
- Support **multi-tenancy** with per-tenant data encryption keys (envelope encryption, master key from env for v1, KMS-ready interface for v2)
- Implement **session lifecycle management** — ACTIVE, EXPIRED, REVOKED, SUSPENDED states with merchant-controlled refresh and revocation webhooks
- Add **Admin API** for tenant registration, tool configuration, session management, audit queries, and key rotation
- Add **health/readiness/metrics** endpoints for operations

## Capabilities

### New Capabilities
- `webhook-session`: Webhook-based session creation, refresh, revocation, and resume — the single entry point for authenticated sessions
- `dpop-tokens`: DPoP-bound token issuance and validation, including bind-code flow for in-app agents
- `mcp-gateway`: MCP server over Streamable HTTP with meta-tool pattern for dynamic, scope-filtered tool discovery and execution
- `reverse-proxy`: Request proxying to merchant upstreams with parameter mapping, Ed25519 request signing, and upstream response forwarding
- `scope-enforcement`: Structured scope and constraint checking against a standard tool catalog on every tool call
- `oauth-flow`: OAuth 2.1 + PKCE authorization flow for external LLM agents with uuid correlation
- `tenant-management`: Multi-tenant admin API for registration, tool config, key rotation, and session oversight
- `audit-logging`: Immutable, per-tenant encrypted audit log for all tool calls and session events
- `crypto-keys`: Ed25519 keypair management, per-tenant data encryption (envelope encryption), and key rotation with grace periods

### Modified Capabilities
_(none — greenfield project)_

## Impact

- **New Go packages**: webhook handling, MCP server, reverse proxy, DPoP validation, scope engine, OAuth flow, admin API, crypto/key management, audit logger
- **Database**: New Postgres schema (tenants, tools, sessions, oauth_pending_flows, audit_logs, tenant_keys)
- **Cache**: Redis for session hot-path, tool config cache, nonce deduplication
- **Dependencies**: Ed25519 (stdlib), JWT library, ECDSA (P-256 for DPoP), River (async audit writes), MCP protocol library or custom Streamable HTTP handler
- **Infrastructure**: Requires Postgres, Redis, and a master encryption key (env var for v1)
- **External integration**: Merchants integrate via webhook signing (Ed25519) and upstream endpoint registration
