---
name: bruno-api-tester
description: >-
  Create comprehensive API test suites using Bruno's OpenCollection YAML format.
  Use this skill whenever the user asks to write API tests, create Bruno collections,
  add test cases for endpoints, test webhooks, or build e2e test suites. Also use it
  when the user mentions Bruno, .bru files, or wants to test REST/GraphQL endpoints
  with assertions and scripting.
---

# Bruno API Tester

You write API test suites using Bruno's OpenCollection YAML format. Your job is to
produce well-structured, thorough test collections that cover happy paths, error
cases, edge cases, and multi-step lifecycle flows.

## Before you start

1. Read `references/bruno-cli.md` in this skill directory for the full Bruno CLI
   and scripting API reference. It covers the YAML format, request/response APIs,
   variable management, assertions, and runner control.
2. Run `bru --help` to discover available CLI commands in the user's environment.
3. Understand the API you're testing — ask for or read OpenAPI specs, handler code,
   or route definitions before writing tests.

## Collection structure

Every test collection follows this layout:

```
e2e/bruno/<collection-name>/
  opencollection.yml          # Collection metadata
  .env                        # Secrets (never committed)
  .env.sample                 # Template for secrets
  .gitignore                  # Must contain: .env
  environments/
    local.yml                 # Default environment with variables
  <N>-<feature>/
    folder.yml                # Folder metadata + shared pre-request scripts
    <N.M>-<test-name>.yml     # Individual test requests
```

### opencollection.yml

```yaml
info:
  name: <Collection Name>
  version: "1"
  type: collection
  description: <What this collection tests>

settings:
  timeout: 5000
  sslVerification: false

runtime:
  scripts:
    - type: before-request
      code: |-
        // Collection-level pre-request: no-op.
        // Signing/auth is handled per-folder or per-request.
```

Keep the collection-level script as a no-op. Push shared auth logic down to
folder-level scripts where it belongs — different endpoint groups often need
different auth strategies.

### environments/local.yml

```yaml
info:
  name: local

vars:
  baseUrl: "{{process.env.BASE_URL}}"
  # Add API-specific variables here
```

Bridge environment variables via `{{process.env.VAR}}` so secrets stay in `.env`
and the YAML files remain safe to commit.

### .env.sample

Document every required environment variable with placeholder values and a comment
explaining how to obtain the real value.

### .gitignore

```
.env
```

## Writing test folders

Group tests by feature or endpoint. Number folders to control execution order:

```
1-authentication/
2-create-resource/
3-update-resource/
4-delete-resource/
5-lifecycle-sequences/
```

### folder.yml

```yaml
info:
  name: <N> - <Feature Name>
  description: <What this group tests>

runtime:
  scripts:
    - type: before-request
      code: |-
        // Shared pre-request logic for all requests in this folder.
        // Common uses: auth header injection, request signing, token refresh.
```

Use the folder-level `before-request` script for auth logic shared by all requests
in the folder (signing, bearer tokens, API keys). This avoids duplicating auth code
in every request file.

## Writing individual tests

### File naming

```
<folder-number>.<sequence>-<descriptive-name>.yml
```

Examples: `2.1-happy-path.yml`, `2.5-missing-required-field.yml`, `4.10-verify-count.yml`

The `seq` field in `info` controls execution order within the folder — it must be
unique per folder and determines the run order when using `bru run <folder>`.

### Request YAML structure

```yaml
info:
  name: "<N.M> <Descriptive test name>"
  type: http
  seq: <integer>

http:
  method: POST  # GET, PUT, PATCH, DELETE
  url: "{{baseUrl}}/your/endpoint"
  body:
    type: json
    data: |-
      {"field":"value"}

runtime:
  scripts:
    - type: tests
      code: |-
        test("should return 200", function() {
          expect(res.status).to.equal(200);
        });
```

### Body types

- **JSON**: `type: json`, `data` is a JSON string
- **No body** (GET/DELETE): omit the `body` key entirely
- **Form**: `type: form-url-encoded`, `data` as key-value pairs
- **XML**: `type: xml`, `data` is an XML string

## Writing good test scripts

Test scripts use Chai's `expect` syntax. Every assertion belongs inside a
`test("description", function() { ... })` block.

### What to assert

For **happy paths**:
- Status code
- Response body structure (required properties exist)
- Value types (string, number, array)
- Value formats (regex for UUIDs, hex strings, ISO dates)
- Business logic (computed values, state transitions)

For **error cases**:
- Status code (400, 401, 403, 404, 409, 422)
- Error code or message in response body
- That no side effects occurred (resource not created)

For **edge cases**:
- Boundary values (max lengths, zero, negative numbers)
- Empty strings, null values, missing fields
- Capped or clamped values (e.g., TTL max enforcement)

### Assertion patterns

```javascript
// Status
expect(res.status).to.equal(201);

// Property existence and type
expect(res.body).to.have.property("id");
expect(res.body.id).to.be.a("string");

// Value matching
expect(res.body.status).to.equal("active");

// Regex (UUIDs, hex, ISO dates)
expect(res.body.id).to.match(/^[0-9a-f-]{36}$/);

// Arrays
expect(res.body.items).to.be.an("array");
expect(res.body.items).to.have.length.greaterThan(0);

// Nested properties
expect(res.body.meta.createdAt).to.be.a("string");

// Negation
expect(res.body).to.not.have.property("password");
```

### Storing and sharing state between requests

Use `bru.setVar()` / `bru.getVar()` to pass data between requests in the same
folder. This is essential for setup-then-test patterns:

```javascript
// In setup test (e.g., 3.0-setup.yml)
bru.setVar("resourceId", res.body.id);

// In subsequent test (e.g., 3.1-get-resource.yml)
// Reference in URL or body: {{resourceId}}
```

Use `bru.setNextRequest("request name")` to chain requests in lifecycle sequences:

```javascript
// After creating a resource, jump to the update step
bru.setNextRequest("3.2 Update resource");
```

### Pre-request scripts for setup

When a test needs specific request-level setup (custom headers, computed values):

```yaml
runtime:
  scripts:
    - type: before-request
      code: |-
        // Request-specific setup
        const id = bru.getVar("resourceId");
        req.setHeader("X-Resource-ID", id);
    - type: tests
      code: |-
        test("should succeed", function() {
          expect(res.status).to.equal(200);
        });
```

## Test coverage strategy

For every endpoint, aim to cover these categories:

### 1. Happy path
The golden scenario where everything is valid. Assert the full response shape.

### 2. Input validation errors
One test per required field missing. One test per field with invalid type/format.
One test for malformed body (not valid JSON, wrong content type).

### 3. Auth/authz errors
Missing credentials, invalid credentials, expired tokens, wrong permissions.

### 4. State-dependent errors
Operating on nonexistent resources (404), resources in wrong state (409),
duplicate creation (409), exceeded limits (429 or business-rule equivalent).

### 5. Edge cases
Boundary values, capped/clamped fields, empty arrays, unicode strings,
very long strings.

### 6. Lifecycle sequences (when applicable)
Multi-step flows that chain requests: create -> update -> verify -> delete.
Use `bru.setNextRequest()` and `bru.setVar()` to build these.

## Common patterns

### Auth via folder-level script

For Bearer token auth:

```javascript
// folder.yml before-request
const token = bru.getEnvVar('API_TOKEN');
req.setHeader('Authorization', `Bearer ${token}`);
req.setHeader('Content-Type', 'application/json');
```

For HMAC/signature-based auth:

```javascript
// folder.yml before-request
const crypto = require('node:crypto');
const secret = bru.getEnvVar('API_SECRET');
const body = req.getBody() ? JSON.stringify(req.getBody()) : '';
const timestamp = Math.floor(Date.now() / 1000).toString();
const signature = crypto.createHmac('sha256', secret)
  .update(`${timestamp}.${body}`)
  .digest('hex');

req.setHeader('X-Signature', signature);
req.setHeader('X-Timestamp', timestamp);
```

### Setup test that creates shared state

```yaml
info:
  name: "3.0 Setup — create resource"
  type: http
  seq: 0

http:
  method: POST
  url: "{{baseUrl}}/resources"
  body:
    type: json
    data: |-
      {"name":"test-resource","type":"example"}

runtime:
  scripts:
    - type: tests
      code: |-
        test("setup: should create resource", function() {
          expect(res.status).to.equal(201);
        });
        bru.setVar("resourceId", res.body.id);
```

### Idempotency test

```yaml
runtime:
  scripts:
    - type: tests
      code: |-
        test("should be idempotent (same result on repeat)", function() {
          expect(res.status).to.equal(200);
          // Same state as first call
          expect(res.body.status).to.equal("deleted");
        });
```

## Things to avoid

- **Don't hardcode IDs or secrets** in request YAML. Use environment variables
  and `bru.setVar()`.
- **Don't duplicate auth logic** across request files. Put it in `folder.yml`.
- **Don't write tests without assertions**. Every test file must assert something.
- **Don't skip error cases**. A test suite with only happy paths gives false confidence.
- **Don't use random data without seeding**. If tests need unique IDs, use a
  deterministic prefix like `"e2e-<feature>-<seq>"`.
