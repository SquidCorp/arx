## ADDED Requirements

### Requirement: Immutable audit log for all events
The system SHALL log every tool call, session lifecycle event, and enforcement decision to an append-only audit log. Log entries SHALL never be modified or deleted through the application (only through database retention policies).

#### Scenario: Tool call logged
- **WHEN** an agent makes a `call_tool` request (whether allowed or denied)
- **THEN** the system SHALL create an audit entry with: tenant_id, session_id, event_type, tool_name, encrypted tool_params, outcome (allowed/denied), deny_reason (if denied), upstream_status (if proxied), latency_ms, ip_address, user_agent, and timestamp

#### Scenario: Session event logged
- **WHEN** a session is created, refreshed, expired, revoked, or suspended
- **THEN** the system SHALL create an audit entry with event_type set to the corresponding session event

### Requirement: Async audit writes via River
Audit log writes SHALL be processed asynchronously through River job queue to avoid adding latency to the tool-call hot path. Writes SHALL have at-least-once delivery guarantees.

#### Scenario: Audit write does not block tool call
- **WHEN** an agent makes a `call_tool` request
- **THEN** the system SHALL enqueue the audit write as a River job and return the tool response without waiting for the audit insert

#### Scenario: River job failure and retry
- **WHEN** a River audit job fails (e.g., Postgres temporarily unavailable)
- **THEN** River SHALL retry the job according to its retry policy until successful

### Requirement: Per-tenant encryption of audit data
Tool call parameters stored in audit logs SHALL be encrypted with the tenant's data encryption key. This ensures tenant data isolation even at the database level.

#### Scenario: Audit params encrypted at rest
- **WHEN** an audit entry is written for a tool call with params `{ productId: "SKU-42" }`
- **THEN** the `tool_params_enc` column SHALL contain ciphertext encrypted with the tenant's DEK, not plaintext

### Requirement: Audit log partitioning
The audit_logs table SHALL be partitioned by month (on `created_at`) to enable efficient queries and retention management.

#### Scenario: Monthly partition exists
- **WHEN** the system writes an audit entry in April 2026
- **THEN** the entry SHALL be stored in the April 2026 partition
