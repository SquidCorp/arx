## ADDED Requirements

### Requirement: Session creation via signed webhook
The system SHALL create authenticated sessions exclusively through `POST /webhook/session-connected`. This endpoint SHALL accept a JSON payload containing `sessionId`, `isAnonymous`, `scopes`, `expiresIn`, and optionally `uuid` (Flow 1), `userId`, `claims`. The system SHALL validate the Ed25519 signature, timestamp freshness (within 5 minutes), and nonce uniqueness before creating the session.

#### Scenario: Successful session creation for Flow 2 (in-app)
- **WHEN** a merchant backend sends a signed `POST /webhook/session-connected` with `sessionId`, `userId`, `scopes`, `expiresIn`, and no `uuid`
- **THEN** the system SHALL validate the signature, create a session with status ACTIVE, and return a `bind_code` with a 60-second TTL

#### Scenario: Successful session creation for Flow 1 (OAuth)
- **WHEN** a merchant backend sends a signed `POST /webhook/session-connected` with a `uuid` matching a pending OAuth flow
- **THEN** the system SHALL create a session, generate an authorization code, link it to the pending OAuth flow, and return `{ status: "pending" }`

#### Scenario: Invalid signature
- **WHEN** the Ed25519 signature on the webhook payload does not match the merchant's registered public key
- **THEN** the system SHALL reject the request with 401 and `"invalid_signature"`

#### Scenario: Stale timestamp
- **WHEN** the `X-AgentGate-Timestamp` header is older than 5 minutes
- **THEN** the system SHALL reject the request with 401 and `"stale_timestamp"`

#### Scenario: Duplicate nonce
- **WHEN** the `X-AgentGate-Nonce` header value has been seen before within the replay window
- **THEN** the system SHALL reject the request with 401 and `"duplicate_nonce"`

#### Scenario: expiresIn exceeds tenant max
- **WHEN** the `expiresIn` value exceeds the tenant's configured `session_max_expiry`
- **THEN** the system SHALL cap the session expiry at `session_max_expiry`

#### Scenario: Unknown uuid
- **WHEN** the `uuid` does not match any pending OAuth flow
- **THEN** the system SHALL reject the request with 404 and `"unknown_flow"`

### Requirement: Session refresh via webhook
The system SHALL support session refresh through `POST /webhook/session-refresh`. The merchant backend MAY update scopes and claims during refresh. The refresh count SHALL be tracked and limited by the tenant's `max_refreshes` configuration.

#### Scenario: Successful refresh
- **WHEN** a merchant sends a signed `POST /webhook/session-refresh` with `sessionId` and `expiresIn` for an ACTIVE session that has not exceeded `max_refreshes`
- **THEN** the system SHALL issue a new token, update the session expiry, increment `refresh_count`, and return the new token

#### Scenario: Refresh with updated scopes
- **WHEN** a refresh request includes a `scopes` field
- **THEN** the system SHALL replace the session's scopes with the new values

#### Scenario: Max refreshes exceeded
- **WHEN** the session's `refresh_count` equals or exceeds the tenant's `max_refreshes`
- **THEN** the system SHALL reject with 403 and `"max_refreshes_exceeded"`

#### Scenario: Refresh on non-active session
- **WHEN** a refresh is attempted on a REVOKED or SUSPENDED session
- **THEN** the system SHALL reject with 409 and `"session_not_active"`

### Requirement: Session revocation via webhook
The system SHALL support immediate session revocation through `POST /webhook/session-revoked`. Revocation MUST be terminal — revoked sessions cannot be refreshed or resumed.

#### Scenario: Successful revocation
- **WHEN** a merchant sends a signed `POST /webhook/session-revoked` with `sessionId`
- **THEN** the system SHALL mark the session as REVOKED, invalidate the Redis cache entry, and return `{ status: "revoked" }`

#### Scenario: Idempotent revocation
- **WHEN** revocation is attempted on an already-revoked session
- **THEN** the system SHALL return 200 with `{ status: "revoked" }`

### Requirement: Session resume via webhook
The system SHALL support resuming SUSPENDED sessions through `POST /webhook/session-resumed`.

#### Scenario: Successful resume
- **WHEN** a merchant sends a signed `POST /webhook/session-resumed` with `sessionId` for a SUSPENDED session
- **THEN** the system SHALL mark the session as ACTIVE and return `{ status: "active" }`

#### Scenario: Resume on non-suspended session
- **WHEN** resume is attempted on a session that is not SUSPENDED
- **THEN** the system SHALL reject with 409 and `"session_not_suspended"`

### Requirement: Session state machine
Sessions SHALL follow strict state transitions: ACTIVE → EXPIRED (automatic on TTL), ACTIVE → REVOKED (via webhook or admin), ACTIVE → SUSPENDED (via security event), SUSPENDED → ACTIVE (via resume webhook), SUSPENDED → REVOKED (via revocation webhook). EXPIRED and REVOKED are terminal states.

#### Scenario: Expired session tool call
- **WHEN** an agent makes a tool call with a token whose session has expired
- **THEN** the system SHALL reject with 401 and `"session_expired"`

#### Scenario: Revoked session tool call
- **WHEN** an agent makes a tool call with a token whose session is REVOKED
- **THEN** the system SHALL reject with 401 and `"session_revoked"`

#### Scenario: Suspended session tool call
- **WHEN** an agent makes a tool call with a token whose session is SUSPENDED
- **THEN** the system SHALL reject with 403 and `"session_suspended"`
