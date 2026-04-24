# E2E Testing Plan — Webhook Session Lifecycle (Tasks 4.1–4.6)

## Prerequisites

- Real Postgres with migrations applied (tenants, sessions, tools, oauth_pending_flows)
- Real Redis instance
- Running HTTP server with routes wired: middleware → handlers
- A seeded tenant with known Ed25519 keypair and session policy (max_expiry=1h, max_refreshes=5)
- Helper to sign requests with the tenant's private key

---

## 1. Middleware — Signature Validation Pipeline

| # | Scenario | Method | Expected |
|---|----------|--------|----------|
| 1.1 | Valid signature, fresh timestamp, unique nonce | POST with all headers correct | 200, request reaches handler |
| 1.2 | Missing `X-Arx-Tenant-ID` | POST, omit header | 400 `missing_tenant_id` |
| 1.3 | Missing `X-Arx-Signature` | POST, omit header | 401 `missing_signature` |
| 1.4 | Missing `X-Arx-Timestamp` | POST, omit header | 401 `missing_timestamp` |
| 1.5 | Missing `X-Arx-Nonce` | POST, omit header | 401 `missing_nonce` |
| 1.6 | Stale timestamp (>5min old) | POST, ts = now - 6min | 401 `stale_timestamp` |
| 1.7 | Future timestamp within window | POST, ts = now + 3min | 200, passes |
| 1.8 | Future timestamp outside window | POST, ts = now + 6min | 401 `stale_timestamp` |
| 1.9 | Duplicate nonce (replay attack) | POST same nonce twice | 1st: 200, 2nd: 401 `duplicate_nonce` |
| 1.10 | Wrong Ed25519 key (tampered sig) | POST, sign with different key | 401 `invalid_signature` |
| 1.11 | Unknown tenant ID | POST, valid sig but tenant not in DB | 401 `unknown_tenant` |
| 1.12 | Malformed base64 signature | POST, garbage in sig header | 401 `invalid_signature_encoding` |
| 1.13 | Body tampered after signing | POST, modify body after signing | 401 `invalid_signature` |

---

## 2. Session Connected — Flow 2 (Direct Bind Code)

| # | Scenario | Payload | Expected |
|---|----------|---------|----------|
| 2.1 | Happy path — create session, get bind code | `{sessionId, scopes, expiresIn}` | 201, `{sessionId, bindCode}`, session in DB with status=active |
| 2.2 | Verify bind code is 64-char hex | As above | `bindCode` matches `^[0-9a-f]{64}$` |
| 2.3 | Verify session cached in Redis | As above | Redis HASH at `session:{id}` exists with correct fields |
| 2.4 | ExpiresIn capped by tenant max (1h) | `expiresIn: 7200` | Session `expires_at` <= now + 1h |
| 2.5 | Missing sessionId | Omit field | 400 `missing_session_id` |
| 2.6 | Missing scopes | Omit field | 400 `missing_scopes` |
| 2.7 | Invalid expiresIn (0 or negative) | `expiresIn: 0` | 400 `invalid_expires_in` |
| 2.8 | Malformed JSON body | `{broken` | 400 `invalid_payload` |

---

## 3. Session Connected — Flow 1 (OAuth Pending Flow)

| # | Scenario | Payload | Expected |
|---|----------|---------|----------|
| 3.1 | Happy path — link to pending flow | `{sessionId, scopes, expiresIn, uuid}` with known uuid | 201, `{status: "pending"}` |
| 3.2 | Session created in DB | As above | Session row exists, status=active, no bind code |
| 3.3 | Session cached in Redis | As above | Redis HASH exists |
| 3.4 | Unknown UUID | `uuid: "nonexistent"` | 404 `unknown_flow` |

---

## 4. Session Refresh

| # | Scenario | Precondition | Expected |
|---|----------|-------------|----------|
| 4.1 | Happy path — refresh active session | Active session, 0 refreshes | 200, `{status: "refreshed"}`, new expiry in DB |
| 4.2 | Refresh with new scopes | Active session | 200, scopes updated in DB and Redis |
| 4.3 | Refresh preserves scopes when not provided | Active session with `[cart.view]` | 200, scopes unchanged |
| 4.4 | Expiry capped by tenant max | `expiresIn: 7200` | `expires_at` <= now + 1h |
| 4.5 | Max refreshes exceeded | Session with `refresh_count=5` | 403 `max_refreshes_exceeded` |
| 4.6 | Refresh revoked session | Revoked session | 409 `session_not_active` |
| 4.7 | Refresh suspended session | Suspended session | 409 `session_not_active` |
| 4.8 | Refresh expired session | Expired session | 409 `session_not_active` |
| 4.9 | Refresh nonexistent session | No session | 404 `session_not_found` |
| 4.10 | Refresh count increments | Active, count=2 | After: count=3 in DB |

---

## 5. Session Revoked

| # | Scenario | Precondition | Expected |
|---|----------|-------------|----------|
| 5.1 | Revoke active session | Active | 200, `{status: "revoked"}`, DB status=revoked |
| 5.2 | Redis cache invalidated | Active, cached | 200, Redis key deleted |
| 5.3 | Idempotent re-revoke | Already revoked | 200, `{status: "revoked"}` (no error) |
| 5.4 | Revoke suspended session | Suspended | 200, status=revoked |
| 5.5 | Revoke expired session | Expired (terminal) | 409 `invalid_state_transition` |
| 5.6 | Revoke nonexistent session | No session | 404 `session_not_found` |

---

## 6. Session Resumed

| # | Scenario | Precondition | Expected |
|---|----------|-------------|----------|
| 6.1 | Resume suspended session | Suspended, not expired | 200, `{status: "active"}`, DB status=active |
| 6.2 | Redis cache re-populated | Suspended, cache empty | 200, Redis HASH recreated with status=active |
| 6.3 | Resume active session | Active | 409 `session_not_suspended` |
| 6.4 | Resume revoked session | Revoked | 409 `session_not_suspended` |
| 6.5 | Resume expired session | Expired | 409 `session_not_suspended` |
| 6.6 | Resume nonexistent session | No session | 404 `session_not_found` |

---

## 7. Full Lifecycle Sequences (Multi-Step E2E)

| # | Scenario | Steps | Expected |
|---|----------|-------|----------|
| 7.1 | Connect → Refresh → Revoke | Create session, refresh 2x, revoke | All succeed, final status=revoked, cache cleared |
| 7.2 | Connect → Suspend → Resume → Revoke | Create, suspend (via DB), resume, revoke | All transitions valid, final=revoked |
| 7.3 | Connect → Revoke → Refresh | Create, revoke, attempt refresh | Refresh returns 409 |
| 7.4 | Connect → Expire → Resume | Create, expire (via DB), attempt resume | Resume returns 409 |
| 7.5 | Connect → Refresh until max → Refresh | Create, refresh 5x, attempt 6th | 6th returns 403 |
| 7.6 | Flow 1 → Refresh → Revoke | Create via OAuth flow, refresh, revoke | Consistent with Flow 2 behavior |

---

## 8. Session State Machine Exhaustive

| # | From → To | Expected |
|---|-----------|----------|
| 8.1 | active → expired | Allowed |
| 8.2 | active → revoked | Allowed |
| 8.3 | active → suspended | Allowed |
| 8.4 | active → active | Rejected |
| 8.5 | suspended → active | Allowed |
| 8.6 | suspended → revoked | Allowed |
| 8.7 | suspended → expired | Rejected |
| 8.8 | suspended → suspended | Rejected |
| 8.9 | expired → any | Rejected (terminal) |
| 8.10 | revoked → any | Rejected (terminal) |

---

## 9. Cache Consistency

| # | Scenario | Expected |
|---|----------|----------|
| 9.1 | Session created → cache read matches DB | Fields identical |
| 9.2 | Session refreshed → cache updated | New expiry and scopes in cache |
| 9.3 | Session revoked → cache deleted | Redis key gone |
| 9.4 | Session resumed → cache re-created | Cache has status=active |
| 9.5 | Redis down during create → falls through to DB | Session still created (cache write is best-effort `_ = ...`) |

---

**Total: 57 test cases** across 9 categories.

The unit tests already cover most handler logic with mocks. The e2e tests should use real Postgres + Redis (via Docker Compose or testcontainers) and hit the actual HTTP routes through the signature validation middleware to verify the full stack end-to-end.
