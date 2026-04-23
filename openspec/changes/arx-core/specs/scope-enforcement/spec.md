## ADDED Requirements

### Requirement: Scope matching on tool calls
The system SHALL verify that the session's scopes are a superset of the tool's `required_scopes` before proxying a tool call. Scope format SHALL be `<resource>:<action>`.

#### Scenario: Sufficient scopes
- **WHEN** a session has scopes `["cart:read", "cart:write", "checkout:exec"]` and the tool requires `["cart:write"]`
- **THEN** the system SHALL allow the tool call

#### Scenario: Insufficient scopes
- **WHEN** a session has scopes `["cart:read"]` and the tool requires `["cart:write"]`
- **THEN** the system SHALL reject with 403 and `"insufficient_scope"` including the missing scope

### Requirement: Structured constraint enforcement
The system SHALL support parameterized constraints in scopes with the format `<resource>:<action>:<key>=<value>`. On tool calls, the system SHALL extract the corresponding parameter from the tool call params and validate it against the constraint.

#### Scenario: Constraint within limit
- **WHEN** a session scope includes `checkout:exec:maxAmount=100:currency=EUR` and the tool call params contain `{ amount: 80, currency: "EUR" }`
- **THEN** the system SHALL allow the tool call

#### Scenario: Constraint exceeded
- **WHEN** a session scope includes `checkout:exec:maxAmount=100` and the tool call params contain `{ amount: 150 }`
- **THEN** the system SHALL reject with 403 and `"constraint_violation"` including `{ constraint: "maxAmount", limit: 100, actual: 150 }`

#### Scenario: Constraint format mismatch
- **WHEN** a constraint expects a numeric value and the tool call params contain a non-numeric value for that field
- **THEN** the system SHALL reject with 400 and a clear error message indicating the expected format

### Requirement: Standard tool catalog
The system SHALL maintain a catalog of standard tool types, each defining a parameter schema with typed fields. Merchants register tools by selecting a catalog type and providing upstream mappings.

#### Scenario: Tool registered with valid catalog type
- **WHEN** a merchant registers a tool with `catalog_type: "cart.add"` that exists in the catalog
- **THEN** the system SHALL accept the registration and store the tool with the catalog's parameter schema

#### Scenario: Unknown catalog type
- **WHEN** a merchant attempts to register a tool with a catalog_type not in the catalog
- **THEN** the system SHALL reject with 400 and `"unknown_catalog_type"`

### Requirement: Scope filtering on tool discovery
The `list_tools` endpoint SHALL only return tools whose `required_scopes` are fully satisfied by the session's scopes.

#### Scenario: Filtered discovery
- **WHEN** a tenant has tools `cart.add` (requires `cart:write`), `cart.view` (requires `cart:read`), and `checkout` (requires `checkout:exec`), and the session scopes are `["cart:read"]`
- **THEN** `list_tools` SHALL return only `cart.view`
