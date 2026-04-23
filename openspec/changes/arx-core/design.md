## Context

Arx is a greenfield Go service that acts as a secure reverse proxy between AI agents and merchant backends. The existing repo has a Go API scaffold with River workers and pgvector, plus Docker Compose with Postgres. Redis needs to be added.

Merchants already handle user authentication via their own systems (Auth0, Clerk, Supabase, etc.). Arx never authenticates users — it receives signed session payloads from merchant backends and issues scoped, DPoP-bound tokens for agents to use.

The system must handle two flows:
1. **External LLM (Flow 1)**: Agent connects via MCP, OAuth 2.1 redirect to merchant login, webhook creates session, token returned via OAuth callback
2. **In-App Agent (Flow 2)**: Merchant backend fires webhook directly, receives bind-code, agent exchanges bind-code for DPoP-bound token

All agent tool calls are proxied through Arx, which enforces scopes, validates DPoP proofs, and logs everything before forwarding to the merchant's upstream endpoints.

## Goals / Non-Goals

**Goals:**
- Secure session creation exclusively via signed merchant webhooks
- DPoP-bound tokens that are useless without the agent's private key
- Sub-2ms enforcement overhead on the tool-call hot path (excluding upstream latency)
- Immutable audit trail for every tool call and session event
- Multi-tenant data isolation via per-tenant encryption
- Standard tool catalog with structured constraint enforcement
- Symmetric Ed25519 trust model (merchants sign to Arx, Arx signs to merchants)

**Non-Goals:**
- Prompt injection scanning (future, separate service)
- Base prompt storage and management (future, client-side)
- Real-time merchant alerting (future)
- User-facing UI or dashboard frontend
- Custom merchant-defined tool schemas (v2 — Option B migration)
- KMS integration for key management (v2 — interface ready, v1 uses local master key)

## Decisions

### D1: MCP meta-tool pattern over dynamic tools/list

Arx exposes 4 static MCP tools (`list_tools`, `get_tool`, `call_tool`, `session_info`) rather than dynamically populating the MCP protocol-level `tools/list` per tenant.

**Rationale**: One MCP server serves all tenants. Tenant-specific tools are discovered via `list_tools` (gated by token/scopes). This avoids per-tenant MCP endpoints, keeps the protocol surface stable, and works naturally with LLMs that handle "call a tool to discover more tools."

**Alternatives considered**:
- Per-tenant MCP endpoints: Operational nightmare at scale, URL management complexity
- Dynamic `tools/list` filtered by token: Requires auth before discovery, which MCP doesn't standardize well; also leaks the number/shape of tools at protocol level

### D2: Streamable HTTP as sole MCP transport

Arx only supports MCP over Streamable HTTP (POST + SSE). No stdio transport.

**Rationale**: Arx is a remote service — stdio would mean running it locally, defeating the purpose. DPoP requires HTTP headers. All major LLM platforms (ChatGPT, Claude) use Streamable HTTP for remote MCP.

### D3: Bind-code pattern for Flow 2 DPoP

For in-app agents (Flow 2), the webhook returns a short-lived bind-code instead of a token directly. The agent generates its own DPoP keypair and exchanges the bind-code for a DPoP-bound token via `POST /token/bind`.

**Rationale**: DPoP's security depends on the private key never leaving the entity that uses it. If the merchant backend received the token and passed it + private key to the agent, the key crosses a process boundary. The bind-code pattern keeps the private key exclusively in the agent's memory.

**Alternatives considered**:
- Direct token return to merchant (pass token + key to agent): Weakens DPoP — key crosses process boundary
- Agent initiates its own session: Violates the "webhook is the only entry point" principle

### D4: Redis hot path + Postgres persistence

Session data and tool configs cached in Redis for the enforcement hot path. Postgres is the source of truth. Write-through on mutations, read-through on cache miss.

**Rationale**: The enforcement pipeline runs on every tool call and must be fast (<2ms). Session lookups, tool resolution, and nonce checks in Redis are ~0.2ms each. Postgres handles durability, audit log persistence, and admin queries.

**Alternatives considered**:
- Postgres only with in-process cache: Viable for low volume but doesn't scale; cache invalidation across instances is harder than Redis
- Redis only: Not durable enough for audit logs and tenant config; no relational queries for admin API

### D5: Ed25519 symmetric trust model

Both directions use Ed25519 signatures over request payloads. Merchants sign webhooks to Arx; Arx signs proxied requests to merchants. Key exchange happens at tenant registration.

**Rationale**: One cryptographic primitive, one mental model. Merchants implementing webhook signing already understand the pattern — verifying Arx's signatures is the same code in reverse. Ed25519 is fast, has small signatures, and is well-supported in Go's stdlib.

**Alternatives considered**:
- Shared API keys for outbound auth: Simpler but key leakage allows impersonation; no cryptographic non-repudiation
- mTLS: Transport-level, no app code needed, but certificate management overhead; many merchant backends aren't configured for it

### D6: Standard tool catalog (Option A) with migration path

Arx defines a catalog of standard tool types with known parameter schemas. Merchants pick from the catalog and provide field mappings to their upstream APIs. Constraint enforcement operates on the standard schema.

**Rationale**: Simplest to implement — Arx knows exactly where every parameter lives because it defined the schema. Migration to Option B (merchant-defined schemas) later only requires letting merchants define custom tool types; the enforcement engine doesn't change.

### D7: Per-tenant envelope encryption with local master key

Each tenant gets a data encryption key (DEK) stored in Postgres, encrypted with a master key from an environment variable. The KeyManager interface abstracts this so KMS can replace the local master key in v2.

**Rationale**: Provides tenant data isolation without external dependencies in v1. The interface boundary (`KeyManager`) means swapping to AWS KMS / GCP KMS / Vault is a drop-in replacement.

### D8: Audit log writes via River (async)

Audit log inserts go through River job queue, non-blocking on the tool-call hot path. The audit_logs table is partitioned by month.

**Rationale**: Audit writes must not add latency to tool calls. River is already in the project and provides at-least-once delivery guarantees. Monthly partitioning enables efficient retention management.

### D9: OAuth refresh for Flow 1, webhook refresh for Flow 2

External LLMs (Flow 1) use standard OAuth refresh_token grant. In-app agents (Flow 2) are refreshed via `POST /webhook/session-refresh` from the merchant backend.

**Rationale**: LLM OAuth clients already implement refresh_token flows — staying spec-compliant avoids custom client logic. For Flow 2, the merchant controls session extension (not the agent), maintaining the principle that merchants are the authority on session validity.

### D10: Session states — ACTIVE, EXPIRED, REVOKED, SUSPENDED

SUSPENDED is a distinct state for security events (not just a flavor of REVOKED). Suspended sessions can be resumed by the merchant via `POST /webhook/session-resumed`.

**Rationale**: Immediate revocation on a potential false positive is disruptive. SUSPENDED lets Arx flag suspicious activity while giving the merchant the final say. The agent gets `403 session_suspended` until the merchant acts.

## Risks / Trade-offs

**[Risk] Redis as single point of failure on hot path** → Mitigation: Redis Sentinel or cluster mode for HA. On Redis failure, fall back to Postgres reads (higher latency but not an outage). Circuit breaker on Redis client.

**[Risk] DPoP nonce storage in Redis could be lost on Redis restart** → Mitigation: Nonces have short TTLs (5 min). A Redis restart means some nonces are forgotten, which could allow a replay within the TTL window. Acceptable for v1; mitigated by the short DPoP proof freshness window (±60 sec).

**[Risk] Standard tool catalog may not fit all merchant use cases** → Mitigation: Design the catalog to be extensible. Option B (merchant-defined schemas) is the planned migration path. The enforcement engine is parameterized, not hardcoded to specific tool types.

**[Risk] Envelope encryption master key in env var** → Mitigation: Restrict access to the deployment environment. Document the KMS migration path. The KeyManager interface ensures v2 migration doesn't require structural changes.

**[Risk] OAuth pending flows could accumulate if merchants don't complete login** → Mitigation: Short TTL (5-10 min) on `oauth_pending_flows` rows. River cron job cleans up expired rows.

**[Risk] Merchant upstream latency affects agent experience** → Mitigation: Configurable per-tool timeout. Arx adds <2ms overhead — the vast majority of latency is the merchant's upstream. Timeout and circuit breaker on outbound proxy calls.

**[Trade-off] Meta-tool pattern means LLMs make 2 calls minimum (list_tools → call_tool)** → Accepted: The extra round-trip is minimal (~1-2ms each) and provides scope-filtered discovery. LLMs handle this pattern well.

**[Trade-off] Bind-code adds a step to Flow 2** → Accepted: The merchant passes a bind-code instead of a token. One extra HTTP call from the agent. Worth it for proper DPoP binding.
