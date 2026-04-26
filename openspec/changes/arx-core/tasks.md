## 1. Foundation & Infrastructure

- [x] 1.1 Add Redis to docker-compose.yaml and update config.yaml with Redis connection settings
- [x] 1.2 Create Postgres migration for `tenants` table with all columns (crypto keys, OAuth config, session policy)
- [x] 1.3 Create Postgres migration for `tools` table with unique constraint on (tenant_id, name)
- [x] 1.4 Create Postgres migration for `sessions` table with indexes on tenant/status, merchant_sid, and expires_at
- [x] 1.5 Create Postgres migration for `oauth_pending_flows` table with unique index on uuid
- [x] 1.6 Create Postgres migration for `audit_logs` partitioned table (by month on created_at) with indexes
- [x] 1.7 Create Postgres migration for `tenant_keys` table for key rotation history

## 2. Crypto & Key Management

- [x] 2.1 Define `KeyManager` interface with `EncryptSessionData`, `DecryptSessionData`, `SignRequest`, `VerifyWebhook` methods
- [x] 2.2 Implement `LocalKeyManager` — loads master key from `ARX_MASTER_KEY` env var, fails on missing/invalid
- [x] 2.3 Implement per-tenant DEK generation (AES-256), encryption with master key, and storage
- [x] 2.4 Implement Ed25519 webhook signature verification (current key + grace period fallback to old key)
- [x] 2.5 Implement Ed25519 outbound request signing (decrypt signing key with DEK, sign payload, zero decrypted key)
- [x] 2.6 Implement key rotation logic — generate new keypair, set grace period on old key, record in tenant_keys

## 3. Redis Cache Layer

- [x] 3.1 Set up Redis client with connection pooling and circuit breaker for fallback to Postgres
- [x] 3.2 Implement session cache — HASH at `session:{id}` with TTL, write-through on create/update, read-through on miss
- [x] 3.3 Implement tool cache — HASH at `tool:{tenant}:{name}` and SET at `tenant:{tenant}:tools`, 5-min TTL
- [x] 3.4 Implement OAuth flow cache — HASH at `oauth:{uuid}`, 10-min TTL
- [x] 3.5 Implement nonce deduplication — `SET nonce:{tenant}:{nonce} NX EX 300` for webhook nonces and `nonce:dpop:{jti}` for DPoP nonces

## 4. Webhook Session Endpoints

- [x] 4.1 Implement webhook signature validation middleware (Ed25519 sig, timestamp freshness ±5min, nonce uniqueness)
- [x] 4.2 Implement `POST /webhook/session-connected` — validate payload, create session, return bind_code (Flow 2) or status:pending (Flow 1)
- [x] 4.3 Implement `POST /webhook/session-refresh` — validate, check max_refreshes, update session, issue new token
- [x] 4.4 Implement `POST /webhook/session-revoked` — mark session REVOKED, invalidate Redis cache, idempotent on re-revoke
- [x] 4.5 Implement `POST /webhook/session-resumed` — mark SUSPENDED session as ACTIVE, reject if not suspended
- [x] 4.6 Implement session state machine enforcement (ACTIVE→EXPIRED/REVOKED/SUSPENDED, SUSPENDED→ACTIVE/REVOKED)

## 5. DPoP Token System

- [x] 5.1 Implement DPoP proof JWT parsing and validation (signature, thumbprint, ath, iat freshness, htm, htu)
- [x] 5.2 Implement DPoP nonce deduplication via Redis (jti uniqueness check, SET NX with 5-min TTL)
- [x] 5.3 Implement access token JWT issuance — EdDSA signed, with iss, sub, aud, exp, iat, cnf.jkt, scope, tenant claims
- [x] 5.4 Implement refresh token JWT issuance (longer expiry, session reference)
- [x] 5.5 Implement `POST /token/bind` — validate bind-code (60s TTL, single-use), require DPoP proof, issue bound token
- [x] 5.6 Implement access token validation middleware — verify signature, extract session, check expiry

## 6. OAuth 2.1 Flow (Flow 1)

- [x] 6.1 Implement `GET /.well-known/oauth-authorization-server` — return metadata with endpoints, PKCE S256, DPoP algs
- [x] 6.2 Implement `GET /oauth/authorize` — validate redirect_uri against tenant config, generate uuid, create pending flow, redirect to merchant login URL
- [x] 6.3 Implement `POST /oauth/token` for `grant_type=authorization_code` — PKCE verification, DPoP binding, issue token + refresh token
- [x] 6.4 Implement `POST /oauth/token` for `grant_type=refresh_token` — validate refresh token, check session state, issue new tokens
- [x] 6.5 Implement `GET /oauth/callback` — match pending flow by uuid, redirect to LLM's redirect_uri with auth code + state
- [x] 6.6 Implement cleanup job (River worker) for expired oauth_pending_flows

## 7. Scope Enforcement Engine

- [x] 7.1 Define standard tool catalog data structure with typed parameter schemas
- [x] 7.2 Implement initial catalog entries (cart.add, cart.remove, cart.view, checkout.exec, orders.list, products.search)
- [x] 7.3 Implement scope matching — check session scopes ⊇ tool required_scopes
- [x] 7.4 Implement structured constraint parsing from scope strings (`resource:action:key=value`)
- [x] 7.5 Implement constraint evaluation — extract param from tool call, compare against constraint (numeric, string, enum)
- [x] 7.6 Implement scope-filtered tool discovery for `list_tools`

## 8. MCP Server

- [x] 8.1 Set up MCP Streamable HTTP transport handler at `/mcp` (POST + SSE)
- [x] 8.2 Implement MCP `initialize` handshake returning server capabilities
- [x] 8.3 Implement MCP `tools/list` returning the 4 static meta-tools with descriptions
- [x] 8.4 Implement `list_tools` tool — require valid token, filter tenant tools by session scopes
- [x] 8.5 Implement `get_tool` tool — require valid token, return full schema for requested tool
- [x] 8.6 Implement `call_tool` tool — require valid token + DPoP proof, run enforcement pipeline, proxy to upstream
- [x] 8.7 Implement `session_info` tool — require valid token, return scopes, claims, expires_at, status

## 9. Reverse Proxy

- [x] 9.1 Implement parameter mapping engine — transform standard catalog params to merchant field paths (body.x, query.x)
- [x] 9.2 Implement outbound HTTP client with per-tool configurable timeout and circuit breaker
- [x] 9.3 Implement Ed25519 request signing on outbound proxied requests (X-Arx-Signature, X-Arx-Timestamp)
- [x] 9.4 Implement session context headers injection (X-Arx-Session, X-Arx-User, X-Arx-Scopes)
- [ ] 9.5 Implement upstream response handling — forward 2xx/4xx responses, mask 5xx errors, handle timeouts

## 10. Audit Logging

- [ ] 10.1 Define River job type for audit log writes with payload structure (tenant, session, event, tool, outcome, etc.)
- [ ] 10.2 Implement audit log River worker — decrypt-free write path (params already encrypted before enqueue)
- [ ] 10.3 Implement audit event emission at all enforcement points (tool call, scope violation, constraint violation, session events)
- [ ] 10.4 Implement per-tenant encryption of tool_params before enqueueing audit jobs

## 11. Admin API

- [ ] 11.1 Implement admin API authentication middleware (API key or JWT, tenant-scoped)
- [ ] 11.2 Implement `POST /admin/v1/tenants` — registration with crypto material generation
- [ ] 11.3 Implement `GET /admin/v1/tenants/:id` and `PATCH /admin/v1/tenants/:id`
- [ ] 11.4 Implement `DELETE /admin/v1/tenants/:id` — deactivate + revoke all sessions
- [ ] 11.5 Implement CRUD for `/admin/v1/tenants/:id/tools` (POST, GET list, GET single, PUT, DELETE)
- [ ] 11.6 Implement `GET /admin/v1/tenants/:id/sessions` with status/userId filters and pagination
- [ ] 11.7 Implement `GET /admin/v1/tenants/:id/sessions/:sid` and `POST .../revoke`
- [ ] 11.8 Implement `GET /admin/v1/tenants/:id/audit` with filters (session, user, tool, event, time range) and pagination
- [ ] 11.9 Implement `POST /admin/v1/tenants/:id/keys/rotate` — generate new keypair, set grace period

## 12. Operations & Health

- [ ] 12.1 Implement `GET /healthz` (liveness), `GET /readyz` (Postgres + Redis connectivity), `GET /metrics` (Prometheus)
- [ ] 12.2 Implement `GET /internal/catalog` — return the standard tool catalog with parameter schemas
- [ ] 12.3 Implement River cron job for session expiry cleanup (mark expired ACTIVE sessions)
- [ ] 12.4 Wire up all routes, middleware, and startup sequence (master key check, DB migration, Redis connection, River workers)
