## ADDED Requirements

### Requirement: OAuth 2.1 metadata discovery
The system SHALL expose OAuth 2.1 authorization server metadata at `GET /.well-known/oauth-authorization-server` including `authorization_endpoint`, `token_endpoint`, supported grant types (`authorization_code`, `refresh_token`), `code_challenge_methods_supported: ["S256"]`, and `dpop_signing_alg_values_supported`.

#### Scenario: Metadata discovery
- **WHEN** an LLM client fetches `GET /.well-known/oauth-authorization-server`
- **THEN** the system SHALL return valid OAuth 2.1 metadata with all required fields

### Requirement: Authorization endpoint with PKCE
The system SHALL accept OAuth authorization requests at `GET /oauth/authorize` with parameters `client_id`, `redirect_uri`, `state`, `code_challenge`, `code_challenge_method`. The system SHALL generate a `uuid`, store a pending OAuth flow, and redirect to the merchant's configured login URL with the uuid.

#### Scenario: Successful authorization redirect
- **WHEN** an LLM sends `GET /oauth/authorize` with valid parameters and the redirect_uri is in the tenant's allowed list
- **THEN** the system SHALL create a pending flow, redirect to `merchant_login_url?agent_session_uuid=<uuid>`

#### Scenario: Invalid redirect_uri
- **WHEN** the `redirect_uri` is not in the tenant's `oauth_redirect_uris` list
- **THEN** the system SHALL reject with 400 and `"invalid_redirect_uri"`

#### Scenario: Pending flow expiry
- **WHEN** a pending OAuth flow is not completed within 10 minutes
- **THEN** the system SHALL mark the flow as expired and reject any subsequent token exchange attempts

### Requirement: Token endpoint with PKCE and DPoP
The system SHALL accept token requests at `POST /oauth/token` supporting `grant_type=authorization_code` (with PKCE code_verifier) and `grant_type=refresh_token`. A DPoP proof header SHALL be required for initial token issuance.

#### Scenario: Successful authorization code exchange
- **WHEN** an LLM sends `POST /oauth/token` with a valid authorization code, code_verifier matching the original code_challenge, and a DPoP proof header
- **THEN** the system SHALL return `{ access_token, refresh_token, token_type: "DPoP", expires_in }`

#### Scenario: Invalid PKCE verifier
- **WHEN** the `code_verifier` does not match the stored `code_challenge` (via S256)
- **THEN** the system SHALL reject with 400 and `"invalid_grant"`

#### Scenario: Refresh token exchange
- **WHEN** an LLM sends `POST /oauth/token` with `grant_type=refresh_token` and a valid refresh token
- **THEN** the system SHALL issue a new access token and refresh token, incrementing the session's refresh count

#### Scenario: Refresh token with expired or revoked session
- **WHEN** a refresh is attempted for a session that is EXPIRED, REVOKED, or has exceeded max_refreshes
- **THEN** the system SHALL reject with 400 and `"invalid_grant"`

### Requirement: OAuth callback completion
The system SHALL expose `GET /oauth/callback` as the redirect target after merchant login. When the merchant webhook has fired and a pending flow has an authorization code, the callback SHALL redirect to the LLM's `redirect_uri` with the authorization code and original state parameter.

#### Scenario: Successful callback
- **WHEN** a user is redirected to `/oauth/callback` and the pending flow has status "connected"
- **THEN** the system SHALL redirect to the LLM's `redirect_uri` with `code=<auth_code>&state=<original_state>`

#### Scenario: Flow not yet connected
- **WHEN** a user reaches `/oauth/callback` but the merchant webhook has not yet fired
- **THEN** the system SHALL wait briefly (polling/SSE) or show a waiting page, and redirect once the webhook fires or timeout after flow expiry
