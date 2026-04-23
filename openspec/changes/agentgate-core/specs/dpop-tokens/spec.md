## ADDED Requirements

### Requirement: DPoP proof validation on every tool call
The system SHALL validate a DPoP proof JWT on every `call_tool` request. Validation SHALL verify: (1) the proof signature matches the public key in the proof header, (2) the JWK thumbprint matches the token's `cnf.jkt` claim, (3) the `ath` claim matches the SHA-256 hash of the access token, (4) the `iat` is within ±60 seconds, (5) the `jti` nonce has not been seen before, (6) `htm` and `htu` match the actual request method and URL.

#### Scenario: Valid DPoP proof
- **WHEN** an agent sends a tool call with a valid access token and a correctly signed, fresh DPoP proof whose thumbprint matches the token's `cnf.jkt`
- **THEN** the system SHALL accept the request and proceed with enforcement

#### Scenario: Thumbprint mismatch (token theft)
- **WHEN** an attacker presents a stolen token with a DPoP proof signed by a different key
- **THEN** the system SHALL reject with 401 and `"invalid_dpop_proof"` because `SHA-256(attacker.jwk) != token.cnf.jkt`

#### Scenario: Replay attack (nonce reuse)
- **WHEN** a previously seen DPoP proof `jti` is submitted
- **THEN** the system SHALL reject with 401 and `"invalid_dpop_proof"`

#### Scenario: Stale proof
- **WHEN** a DPoP proof's `iat` is more than 60 seconds from the server's current time
- **THEN** the system SHALL reject with 401 and `"invalid_dpop_proof"`

#### Scenario: Token hash mismatch
- **WHEN** the DPoP proof's `ath` claim does not match `SHA-256(access_token)`
- **THEN** the system SHALL reject with 401 and `"invalid_dpop_proof"`

### Requirement: DPoP-bound token issuance
The system SHALL issue access tokens as JWTs with a `cnf.jkt` claim containing the SHA-256 thumbprint of the agent's public key. Tokens SHALL use `token_type: "DPoP"` and be signed with AgentGate's EdDSA key.

#### Scenario: Token issued during OAuth flow
- **WHEN** an agent exchanges an authorization code at `POST /oauth/token` with a DPoP proof header
- **THEN** the system SHALL issue an access token with `cnf.jkt` set to the SHA-256 thumbprint of the proof's JWK

#### Scenario: Token issued via bind-code
- **WHEN** an agent exchanges a bind-code at `POST /token/bind` with a DPoP proof header
- **THEN** the system SHALL issue an access token with `cnf.jkt` set to the SHA-256 thumbprint of the proof's JWK

### Requirement: Bind-code flow for in-app agents
The system SHALL support a bind-code exchange at `POST /token/bind` where the agent presents a one-time bind-code and a DPoP proof to receive a DPoP-bound access token. The bind-code SHALL have a 60-second TTL and be single-use.

#### Scenario: Successful bind-code exchange
- **WHEN** an agent sends `POST /token/bind` with a valid, unexpired bind-code and a DPoP proof
- **THEN** the system SHALL issue a DPoP-bound access token and invalidate the bind-code

#### Scenario: Expired bind-code
- **WHEN** an agent attempts to exchange a bind-code older than 60 seconds
- **THEN** the system SHALL reject with 400 and `"bind_code_expired"`

#### Scenario: Reused bind-code
- **WHEN** an agent attempts to exchange a bind-code that has already been used
- **THEN** the system SHALL reject with 400 and `"bind_code_already_used"`

### Requirement: Access token structure
Access tokens SHALL be JWTs containing: `iss` (AgentGate URL), `sub` (session ID), `aud` ("agentgate"), `exp`, `iat`, `cnf.jkt` (DPoP thumbprint), `scope` (space-separated), and `tenant` (tenant ID).

#### Scenario: Token contains required claims
- **WHEN** AgentGate issues an access token
- **THEN** the token SHALL contain all required claims: `iss`, `sub`, `aud`, `exp`, `iat`, `cnf.jkt`, `scope`, `tenant`

### Requirement: Nonce deduplication via Redis
DPoP proof nonces (`jti`) SHALL be tracked in Redis with a 5-minute TTL to prevent replay attacks.

#### Scenario: Nonce stored on valid proof
- **WHEN** a DPoP proof passes all validation checks
- **THEN** the system SHALL store its `jti` in Redis with a 5-minute TTL using SET NX

#### Scenario: Redis unavailable during nonce check
- **WHEN** Redis is unavailable for nonce deduplication
- **THEN** the system SHALL fall back to rejecting the request with 503 and `"service_unavailable"` rather than skipping the nonce check
