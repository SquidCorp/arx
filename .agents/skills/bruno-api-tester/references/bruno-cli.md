# Bruno Documentation

Bruno is a Git-friendly, offline-first, open-source API client designed for developers who want fast local workflows, plain-text collections, and better collaboration through Git. Unlike cloud-based API clients, Bruno stores API collections as human-readable files (YAML or Bru format) directly in your filesystem, enabling version control, code reviews, and team collaboration through standard Git workflows.

The platform supports REST, GraphQL, gRPC, WebSocket, and SOAP protocols with powerful scripting capabilities using JavaScript. Bruno features a comprehensive variable system, multiple authentication methods (OAuth 2.0, Basic, Bearer, Digest, AWS Signature), test automation with assertions, and a CLI for CI/CD integration. Collections can be stored in either the OpenCollection YAML format (recommended for new projects) or the custom Bru markup language, both designed for readability and version control compatibility.

## CLI Installation and Basic Usage

Install the Bruno CLI globally via npm to run API collections from the command line, enabling automation and CI/CD integration.

```bash
# Install Bruno CLI globally
npm install -g @usebruno/cli

# Verify installation
bru --version

# Navigate to your collection directory
cd /path/to/my-collection

# Run all requests in the collection
bru run

# Run a specific folder within the collection
bru run users

# Run a single request file
bru run create-user.bru

# Run with a specific environment
bru run --env production

# Run with Developer Mode (enables external npm packages and filesystem access)
bru run --sandbox=developer

# Run with environment variable overrides
bru run --env local --env-var JWT_TOKEN=abc123 --env-var API_KEY=xyz789
```

## CLI Data-Driven Testing

Run collections with external data sources (CSV or JSON) for parameterized testing across multiple data sets.

```bash
# Run collection with CSV data (executes once per row)
bru run --csv-file-path ./test-data/users.csv

# Run collection with JSON data file
bru run --json-file-path ./test-data/scenarios.json

# Run collection multiple times
bru run --iteration-count 5

# Run in parallel mode for faster execution
bru run --iteration-count 10 --parallel

# Filter requests by tags
bru run --tags=smoke,sanity

# Exclude certain requests by tags
bru run --exclude-tags=skip,draft

# Combined tag filtering
bru run --tags=api-tests --exclude-tags=slow,experimental
```

## CLI Report Generation

Generate test reports in multiple formats for CI/CD pipelines and documentation.

```bash
# Generate JSON report
bru run --reporter-json ./reports/results.json

# Generate JUnit XML report (for CI/CD tools like Jenkins, GitLab CI)
bru run --reporter-junit ./reports/junit.xml

# Generate HTML report for human-readable output
bru run --reporter-html ./reports/report.html

# Generate multiple reports simultaneously
bru run --reporter-json ./results.json --reporter-junit ./junit.xml --reporter-html ./report.html

# Skip sensitive data from reports
bru run --reporter-json ./results.json --reporter-skip-headers authorization,x-api-key
bru run --reporter-json ./results.json --reporter-skip-request-body --reporter-skip-response-body
```

## OpenCollection YAML Format

The recommended format for new collections. Store requests as standard YAML files with full IDE support and tooling integration.

```yaml
# File: create-user.yml
info:
  name: Create User
  type: http
  seq: 1

http:
  method: POST
  url: https://api.example.com/users
  headers:
    - name: Content-Type
      value: application/json
    - name: Authorization
      value: Bearer {{access_token}}
  body:
    type: json
    data: |-
      {
        "name": "{{userName}}",
        "email": "{{userEmail}}",
        "role": "developer"
      }

runtime:
  scripts:
    - type: before-request
      code: |-
        const timestamp = Date.now();
        bru.setVar("requestTime", timestamp);
        console.log("Creating user at:", timestamp);
    - type: after-response
      code: |-
        bru.setVar("userId", res.body.id);
        bru.setEnvVar("lastCreatedUser", res.body.id);
    - type: tests
      code: |-
        test("should return 201 Created", function() {
          expect(res.status).to.equal(201);
        });

        test("should return user with ID", function() {
          expect(res.body.id).to.be.a('string');
          expect(res.body.name).to.equal(bru.getVar("userName"));
        });

  assertions:
    - expression: res.status
      operator: eq
      value: "201"
    - expression: res.body.id
      operator: isNotEmpty

settings:
  encodeUrl: true
  timeout: 5000
  followRedirects: true
  maxRedirects: 5
```

## Bru Markup Language Format

The original Bruno file format using a custom domain-specific language with dictionary, text, and array blocks.

```bru
meta {
  name: Create User
  type: http
  seq: 1
}

post {
  url: https://api.example.com/users
  body: json
  auth: bearer
}

headers {
  content-type: application/json
  x-request-id: {{$guid}}
  ~x-debug: true
}

auth:bearer {
  token: {{access_token}}
}

body:json {
  {
    "name": "{{userName}}",
    "email": "{{userEmail}}",
    "role": "developer"
  }
}

vars:pre-request {
  requestTime: {{$timestamp}}
}

script:pre-request {
  console.log("Creating user:", bru.getVar("userName"));
}

script:post-response {
  bru.setVar("userId", res.body.id);
}

tests {
  test("should return 201 Created", function() {
    expect(res.status).to.equal(201);
  });

  test("should have valid response structure", function() {
    expect(res.body).to.have.property("id");
    expect(res.body.name).to.equal(bru.getVar("userName"));
  });
}

assert {
  res.status: eq 201
  res.body.id: isString
}
```

## Request Object API (Pre-Request Scripts)

The `req` object provides methods to access and modify request properties before execution.

```javascript
// Pre-request script examples

// URL manipulation
const currentUrl = req.getUrl();
req.setUrl("https://api.example.com/v2/users");
const host = req.getHost();      // "api.example.com"
const path = req.getPath();      // "/v2/users"
const query = req.getQueryString(); // "page=1&limit=10"

// Method manipulation
const method = req.getMethod();
req.setMethod("PUT");

// Header manipulation
req.setHeader("Authorization", "Bearer " + bru.getEnvVar("token"));
req.setHeader("Content-Type", "application/json");
req.setHeaders({
  "X-Request-ID": bru.interpolate("{{$guid}}"),
  "X-Timestamp": Date.now().toString()
});

const authHeader = req.getHeader("Authorization");
const allHeaders = req.getHeaders();
req.deleteHeader("X-Debug");
req.deleteHeaders(["X-Temp", "X-Test"]);

// Body manipulation
const body = req.getBody();
req.setBody({
  username: bru.getEnvVar("username"),
  timestamp: Date.now(),
  requestId: bru.interpolate("{{$guid}}")
});

// Request configuration
req.setTimeout(10000);  // 10 seconds
req.setMaxRedirects(3);
const timeout = req.getTimeout();

// Request metadata
const name = req.getName();
const tags = req.getTags();
const authMode = req.getAuthMode();
const platform = req.getExecutionPlatform(); // "app" or "cli"
const mode = req.getExecutionMode();         // "runner" or "standalone"

// Conditional logic based on tags
if (tags.includes("requires-auth")) {
  const token = bru.getEnvVar("access_token");
  req.setHeader("Authorization", "Bearer " + token);
}

// Error handling (Developer Mode only)
req.onFail((error) => {
  console.error("Request failed:", error.message);
  bru.setVar("lastError", error.message);
});
```

## Response Object API (Post-Response Scripts)

The `res` object contains all response information after request execution.

```javascript
// Post-response script examples

// Direct property access
console.log("Status:", res.status);           // 200
console.log("Status Text:", res.statusText);  // "OK"
console.log("Response Time:", res.responseTime); // 245 (ms)
console.log("Final URL:", res.url);

// Response body (auto-parsed as JSON if applicable)
const data = res.body;
console.log("User ID:", data.id);
console.log("User Name:", data.name);

// Method-based access
const status = res.getStatus();
const statusText = res.getStatusText();
const body = res.getBody();
const responseTime = res.getResponseTime();
const url = res.getUrl();

// Headers
const contentType = res.getHeader("content-type");
const allHeaders = res.getHeaders();
console.log("Rate Limit Remaining:", res.getHeader("x-rate-limit-remaining"));

// Response size
const size = res.getSize();
console.log("Body size:", size.body, "bytes");
console.log("Headers size:", size.headers, "bytes");
console.log("Total size:", size.total, "bytes");

// Store response data for subsequent requests
bru.setVar("userId", res.body.id);
bru.setVar("authToken", res.body.token);
bru.setEnvVar("lastResponseTime", res.responseTime);

// Modify response body (for downstream processing)
res.setBody({
  ...res.body,
  processedAt: new Date().toISOString()
});
```

## Environment Variables API

Manage environment-specific configuration and secrets across requests.

```javascript
// Get current environment name
const envName = bru.getEnvName();
console.log("Running in:", envName); // "production", "staging", etc.

// Check and get environment variables
if (bru.hasEnvVar("api_key")) {
  const apiKey = bru.getEnvVar("api_key");
  req.setHeader("X-API-Key", apiKey);
}

// Set environment variables (in-memory by default)
bru.setEnvVar("session_id", "abc123");

// Persist environment variable to disk
bru.setEnvVar("refresh_token", res.body.refresh_token, { persist: true });

// Get all environment variables
const allEnvVars = bru.getAllEnvVars();
console.log("Environment config:", JSON.stringify(allEnvVars));

// Delete environment variables
bru.deleteEnvVar("temp_token");
bru.deleteAllEnvVars(); // Clear all

// Global/Workspace environment variables
const globalApiUrl = bru.getGlobalEnvVar("base_url");
bru.setGlobalEnvVar("global_counter", 1);
const allGlobalVars = bru.getAllGlobalEnvVars();

// Process environment variables (from .env file)
const secretKey = bru.getProcessEnv("SECRET_API_KEY");
const dbPassword = bru.getProcessEnv("DATABASE_PASSWORD");
```

## Runtime Variables API

Temporary variables for passing data between requests during a collection run.

```javascript
// Set runtime variables (exist only during execution)
bru.setVar("userId", res.body.id);
bru.setVar("orderTotal", 99.99);
bru.setVar("items", ["item1", "item2", "item3"]);

// Check and get runtime variables
if (bru.hasVar("userId")) {
  const userId = bru.getVar("userId");
  req.setUrl(`https://api.example.com/users/${userId}`);
}

// Get all runtime variables
const allVars = bru.getAllVars();
console.log("Runtime state:", JSON.stringify(allVars));

// Delete runtime variables
bru.deleteVar("tempData");
bru.deleteAllVars(); // Clear all runtime variables

// Access different variable scopes
const collectionVar = bru.getCollectionVar("baseUrl");
const folderVar = bru.getFolderVar("resourceId");
const requestVar = bru.getRequestVar("specificValue");
const collectionName = bru.getCollectionName();

// Check collection variable existence
if (bru.hasCollectionVar("apiVersion")) {
  console.log("API Version:", bru.getCollectionVar("apiVersion"));
}
```

## Test Scripts with Chai Assertions

Write comprehensive tests using the built-in Chai assertion library.

```javascript
// Basic status code tests
test("should return 200 OK", function() {
  expect(res.status).to.equal(200);
});

test("should return success status", function() {
  expect(res.status).to.be.oneOf([200, 201, 204]);
});

// Response body tests
test("should return user data", function() {
  expect(res.body).to.have.property("id");
  expect(res.body).to.have.property("name");
  expect(res.body).to.have.property("email");
});

test("should return array of users", function() {
  expect(res.body.users).to.be.an("array");
  expect(res.body.users).to.have.length.greaterThan(0);
  expect(res.body.users[0]).to.have.property("id");
});

// Type checking
test("should have correct data types", function() {
  expect(res.body.id).to.be.a("string");
  expect(res.body.age).to.be.a("number");
  expect(res.body.active).to.be.a("boolean");
  expect(res.body.tags).to.be.an("array");
  expect(res.body.metadata).to.be.an("object");
});

// String assertions
test("should have valid email format", function() {
  expect(res.body.email).to.include("@");
  expect(res.body.email).to.match(/^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$/);
});

// Numeric assertions
test("should have valid numeric values", function() {
  expect(res.body.price).to.be.above(0);
  expect(res.body.quantity).to.be.at.least(1);
  expect(res.body.discount).to.be.below(100);
  expect(res.body.rating).to.be.within(1, 5);
});

// Nested object testing
test("should have valid nested structure", function() {
  expect(res.body.user.profile.address.city).to.equal("New York");
  expect(res.body.order.items[0].price).to.be.a("number");
});

// Response time assertions
test("should respond within acceptable time", function() {
  expect(res.responseTime).to.be.below(2000);
});

// Header assertions
test("should return correct content type", function() {
  expect(res.headers["content-type"]).to.include("application/json");
});

// Deep equality
test("should match expected structure", function() {
  expect(res.body.config).to.deep.equal({
    theme: "dark",
    notifications: true
  });
});
```

## Declarative Assertions

Use the Assert tab for code-free testing with built-in operators.

```yaml
# Common assertion patterns (in YAML format)
runtime:
  assertions:
    # Status code checks
    - expression: res.status
      operator: eq
      value: "200"

    # String checks
    - expression: res.body.message
      operator: contains
      value: "success"

    - expression: res.body.email
      operator: endsWith
      value: "@example.com"

    # Existence checks
    - expression: res.body.id
      operator: isNotEmpty

    - expression: res.body.deletedAt
      operator: isNull

    # Type checks
    - expression: res.body.count
      operator: isNumber

    - expression: res.body.items
      operator: isArray

    # Comparison checks
    - expression: res.body.price
      operator: gt
      value: "0"

    - expression: res.responseTime
      operator: lt
      value: "1000"

    # Array/nested access
    - expression: res.body.users[0].name
      operator: eq
      value: "Alice"

    # Using res() query function for complex paths
    - expression: res('order.items[0].price')
      operator: eq
      value: "29.99"
```

## Runner Control API

Control collection run flow, skip requests, or stop execution.

```javascript
// Skip the current request conditionally (pre-request script)
const tags = req.getTags();
if (tags.includes("skip-in-ci") && req.getExecutionPlatform() === "cli") {
  bru.runner.skipRequest();
}

// Skip based on environment
if (bru.getEnvName() === "production") {
  console.log("Skipping destructive test in production");
  bru.runner.skipRequest();
}

// Set the next request to execute (post-response/test script)
if (res.body.requiresVerification) {
  bru.setNextRequest("Verify Email");
} else {
  bru.setNextRequest("Get User Profile");
}

// Stop collection run entirely
if (res.status === 401) {
  console.error("Authentication failed, stopping run");
  bru.runner.stopExecution();
}

// Conditional flow based on response
test("handle authentication flow", function() {
  if (res.body.mfaRequired) {
    bru.setVar("mfaToken", res.body.mfaToken);
    bru.setNextRequest("Submit MFA Code");
  } else {
    bru.setVar("sessionToken", res.body.token);
    bru.setNextRequest("Fetch Dashboard");
  }
});

// Stop after specific condition
if (bru.getVar("errorCount") > 3) {
  console.log("Too many errors, aborting run");
  bru.setNextRequest(null); // Stops the run
}
```

## Programmatic HTTP Requests

Send additional HTTP requests within scripts for complex workflows.

```javascript
// Basic programmatic request with callback
await bru.sendRequest({
  method: "POST",
  url: "https://api.example.com/auth/token",
  headers: {
    "Content-Type": "application/json"
  },
  data: {
    client_id: bru.getEnvVar("client_id"),
    client_secret: bru.getProcessEnv("CLIENT_SECRET"),
    grant_type: "client_credentials"
  }
}, function(err, res) {
  if (err) {
    console.error("Auth failed:", err.message);
    return;
  }
  bru.setEnvVar("access_token", res.data.access_token, { persist: true });
  bru.setVar("tokenExpiry", Date.now() + (res.data.expires_in * 1000));
});

// Async/await pattern
const authResponse = await bru.sendRequest({
  method: "POST",
  url: "https://api.example.com/login",
  headers: { "Content-Type": "application/json" },
  data: { username: "test", password: "secret" },
  timeout: 5000
});
console.log("Login response:", authResponse.data);

// Run another request from the collection
const userResponse = await bru.runRequest("users/get-current-user");
bru.setVar("currentUserId", userResponse.body.id);

// Custom HTTPS agent for self-signed certificates (Developer Mode)
const https = require("node:https");
const agent = new https.Agent({ rejectUnauthorized: false });
const response = await bru.sendRequest({
  url: "https://self-signed.local/api",
  method: "GET",
  httpsAgent: agent
});
```

## Cookie Management

Manage cookies programmatically for session handling and authentication.

```javascript
// Create a cookie jar instance
const jar = bru.cookies.jar();

// Set individual cookies
jar.setCookie("https://api.example.com", "sessionId", "abc123");

// Set cookie with full options
jar.setCookie("https://api.example.com", {
  key: "authToken",
  value: "xyz789",
  domain: "api.example.com",
  path: "/api",
  secure: true,
  httpOnly: true,
  maxAge: 3600,
  sameSite: "Strict"
});

// Set multiple cookies at once
jar.setCookies("https://api.example.com", [
  { key: "userId", value: "user123", secure: true },
  { key: "preferences", value: "dark-mode", path: "/", maxAge: 86400 }
]);

// Get cookies
const sessionCookie = await jar.getCookie("https://api.example.com", "sessionId");
console.log("Session:", sessionCookie?.value);

const allCookies = await jar.getCookies("https://api.example.com");
console.log("All cookies:", allCookies.map(c => c.key));

// Check cookie existence
const hasSession = await jar.hasCookie("https://api.example.com", "sessionId");
if (!hasSession) {
  console.log("No session cookie, need to authenticate");
}

// Delete cookies
jar.deleteCookie("https://api.example.com", "tempToken");
jar.deleteCookies("https://api.example.com"); // Delete all for URL
jar.clear(); // Clear entire jar
```

## Utility Functions

Helper functions for common scripting tasks.

```javascript
// Pause execution
await bru.sleep(2000); // Wait 2 seconds

// Interpolate dynamic variables
const greeting = bru.interpolate("Hello {{$randomFirstName}}!");
const userData = bru.interpolate(`
  Name: {{$randomFullName}}
  Email: {{$randomEmail}}
  Company: {{$randomCompanyName}}
`);

// Get current working directory
const collectionPath = bru.cwd();
console.log("Collection located at:", collectionPath);

// Check sandbox mode
if (bru.isSafeMode()) {
  console.log("Running in Safe Mode - limited capabilities");
} else {
  // Developer Mode features available
  const fs = require("fs");
  const config = JSON.parse(fs.readFileSync(bru.cwd() + "/config.json", "utf8"));
}

// Disable automatic JSON parsing for response
bru.disableParsingResponseJson();

// Get test and assertion results
const testResults = await bru.getTestResults();
const assertionResults = await bru.getAssertionResults();
console.log("Tests passed:", testResults.filter(t => t.passed).length);

// Access secrets from secret managers
const apiKey = bru.getSecretVar("payment-service.api-key");
req.setHeader("X-API-Key", apiKey);

// OAuth2 credential management
const accessToken = bru.getOauth2CredentialVar("access_token");
bru.resetOauth2Credential("my-oauth-credential"); // Force re-authentication
```

## Environment File (.env) Configuration

Store secrets outside version control using dotenv files.

```bash
# File: .env (at collection root - add to .gitignore)
JWT_SECRET=your-super-secret-jwt-key
API_KEY=prod-api-key-12345
DATABASE_URL=postgresql://user:pass@localhost:5432/db
STRIPE_SECRET_KEY=sk_live_xxxxx

# File: .env.sample (commit this for team reference)
JWT_SECRET=your-jwt-secret-here
API_KEY=your-api-key-here
DATABASE_URL=your-database-url-here
STRIPE_SECRET_KEY=your-stripe-key-here
```

```bru
# File: environments/production.bru
vars {
  baseUrl: https://api.production.example.com
  apiKey: {{process.env.API_KEY}}
  jwtSecret: {{process.env.JWT_SECRET}}
}

vars:secret [
  apiKey,
  jwtSecret
]
```

```javascript
// Access in scripts
const apiKey = bru.getProcessEnv("API_KEY");
const dbUrl = bru.getProcessEnv("DATABASE_URL");

// Handle variables with dots in names
// .env: example.test=myvalue
const value = "{{process.env['example.test']}}"; // Use bracket notation
```

## Collection Structure

Organize your API collections with proper folder structure for both YAML and Bru formats.

```
# OpenCollection YAML structure (recommended)
my-api-collection/
├── opencollection.yml          # Collection metadata and settings
├── .env                        # Secrets (gitignored)
├── .env.sample                 # Template for team
├── environments/
│   ├── development.yml
│   ├── staging.yml
│   └── production.yml
├── auth/
│   ├── folder.yml              # Folder-level settings/scripts
│   ├── login.yml
│   ├── refresh-token.yml
│   └── logout.yml
├── users/
│   ├── folder.yml
│   ├── create-user.yml
│   ├── get-user.yml
│   ├── update-user.yml
│   └── delete-user.yml
└── orders/
    ├── folder.yml
    ├── create-order.yml
    └── list-orders.yml

# Bru format structure
my-api-collection/
├── bruno.json                  # Collection configuration
├── collection.bru              # Collection-level scripts
├── .env
├── environments/
│   ├── development.bru
│   └── production.bru
├── auth/
│   ├── folder.bru
│   ├── login.bru
│   └── logout.bru
└── users/
    ├── folder.bru
    ├── create-user.bru
    └── get-user.bru
```

Bruno serves as a comprehensive API development and testing platform that integrates seamlessly with modern development workflows. Its Git-friendly file-based architecture enables teams to version control their API collections alongside application code, facilitating code reviews for API changes and ensuring consistency across development environments. The powerful scripting capabilities with JavaScript, combined with the extensive variable system and authentication support, make it suitable for complex API testing scenarios including OAuth flows, chained requests, and data-driven testing.

The CLI tool enables automation in CI/CD pipelines, supporting parallel execution, multiple report formats (JSON, JUnit, HTML), and integration with tools like Jenkins and GitHub Actions. Whether used for manual API exploration during development, automated regression testing, or generating API documentation, Bruno provides a complete toolkit while maintaining the principle of local-first, privacy-respecting software that doesn't require cloud accounts or subscriptions for core functionality.
