## ADDED Requirements

### Requirement: Tenant registration
The system SHALL support tenant creation via `POST /admin/v1/tenants` accepting tenant name, merchant public key, OAuth config, and session policy. The system SHALL generate a tenant ID, per-tenant encryption key, signing keypair, and return the tenant ID, AgentGate public key, and MCP endpoint URL.

#### Scenario: Successful tenant registration
- **WHEN** an admin sends `POST /admin/v1/tenants` with valid configuration
- **THEN** the system SHALL create the tenant, generate crypto material, and return `{ tenant_id, agentgate_public_key, mcp_endpoint_url }`

#### Scenario: Duplicate tenant name
- **WHEN** a tenant with the same name already exists
- **THEN** the system SHALL reject with 409 and `"tenant_already_exists"`

### Requirement: Tenant configuration management
The system SHALL support reading (`GET`), updating (`PATCH`), and deactivating (`DELETE`) tenant configurations via the admin API. Deactivation SHALL revoke all active sessions for the tenant.

#### Scenario: Update tenant config
- **WHEN** an admin sends `PATCH /admin/v1/tenants/:id` with updated session policy
- **THEN** the system SHALL update the configuration; existing sessions are unaffected, new sessions use new policy

#### Scenario: Deactivate tenant
- **WHEN** an admin sends `DELETE /admin/v1/tenants/:id`
- **THEN** the system SHALL mark the tenant as deactivated and revoke all ACTIVE and SUSPENDED sessions

### Requirement: Tool registration and management
The system SHALL support CRUD operations on tools via the admin API under `/admin/v1/tenants/:id/tools`. Each tool MUST reference a valid catalog type and include upstream URL, method, param mapping, and required scopes.

#### Scenario: Register a tool
- **WHEN** an admin sends `POST /admin/v1/tenants/:id/tools` with a valid catalog type and upstream config
- **THEN** the system SHALL create the tool and make it available for discovery and execution

#### Scenario: Delete a tool
- **WHEN** an admin sends `DELETE /admin/v1/tenants/:id/tools/:name`
- **THEN** the system SHALL disable the tool; subsequent `call_tool` and `get_tool` requests for it SHALL return 404

#### Scenario: List tools
- **WHEN** an admin sends `GET /admin/v1/tenants/:id/tools`
- **THEN** the system SHALL return all registered tools for the tenant (including disabled ones)

### Requirement: Session management via admin API
The system SHALL support listing and viewing sessions (`GET /admin/v1/tenants/:id/sessions`), viewing session details (`GET .../sessions/:sid`), and admin-initiated revocation (`POST .../sessions/:sid/revoke`).

#### Scenario: List sessions with filters
- **WHEN** an admin sends `GET /admin/v1/tenants/:id/sessions?status=active&userId=user-123`
- **THEN** the system SHALL return paginated sessions matching the filters

#### Scenario: Admin revocation
- **WHEN** an admin sends `POST /admin/v1/tenants/:id/sessions/:sid/revoke`
- **THEN** the system SHALL revoke the session (same effect as webhook revocation)

### Requirement: Audit log query
The system SHALL expose `GET /admin/v1/tenants/:id/audit` with filters for session, user, tool, event type, outcome, and time range. Results SHALL be paginated and read-only.

#### Scenario: Query audit log by time range
- **WHEN** an admin sends `GET /admin/v1/tenants/:id/audit?from=2026-04-01&to=2026-04-22`
- **THEN** the system SHALL return paginated audit entries within the time range

### Requirement: Key rotation
The system SHALL support rotating the AgentGate signing keypair for a tenant via `POST /admin/v1/tenants/:id/keys/rotate`. The old key SHALL remain valid for a configurable grace period (default 24 hours).

#### Scenario: Successful key rotation
- **WHEN** an admin triggers key rotation for a tenant
- **THEN** the system SHALL generate a new keypair, return the new public key, and keep the old key valid for the grace period

#### Scenario: Grace period expiry
- **WHEN** the grace period for an old key expires
- **THEN** the system SHALL stop accepting signatures made with the old key

### Requirement: Admin API authentication
The admin API SHALL require authentication via API key or JWT. Each API key/JWT SHALL be scoped to a specific tenant. Tenant-scoped credentials SHALL only access that tenant's resources.

#### Scenario: Valid admin credentials
- **WHEN** an admin sends a request with a valid API key for tenant "acme"
- **THEN** the system SHALL allow access to tenant "acme" resources only

#### Scenario: Cross-tenant access attempt
- **WHEN** an admin with credentials for tenant "acme" attempts to access tenant "beta" resources
- **THEN** the system SHALL reject with 403
