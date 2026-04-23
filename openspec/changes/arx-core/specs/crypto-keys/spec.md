## ADDED Requirements

### Requirement: KeyManager interface
The system SHALL abstract all encryption and signing operations behind a `KeyManager` interface supporting: `EncryptSessionData`, `DecryptSessionData`, `SignRequest`, and `VerifyWebhook`. This enables swapping the v1 local implementation for a KMS-backed implementation in v2.

#### Scenario: Interface used for all crypto operations
- **WHEN** the system encrypts session claims, decrypts audit params, signs outbound requests, or verifies inbound webhooks
- **THEN** the system SHALL use the KeyManager interface, never direct crypto primitives

### Requirement: Per-tenant data encryption keys (DEK)
Each tenant SHALL have a unique data encryption key (DEK) used for encrypting session claims, audit log parameters, and any other tenant-specific sensitive data. The DEK SHALL be stored in Postgres, encrypted with the master key.

#### Scenario: Tenant DEK generated at registration
- **WHEN** a new tenant is registered
- **THEN** the system SHALL generate a random AES-256 DEK, encrypt it with the master key, and store the ciphertext in the tenant record

#### Scenario: DEK used for session claims
- **WHEN** a session is created with claims `{ loyaltyTier: "gold" }`
- **THEN** the system SHALL encrypt the claims JSON with the tenant's DEK before storing in Postgres

### Requirement: Master key from environment
In v1, the master key for encrypting DEKs SHALL be loaded from an environment variable (`ARX_MASTER_KEY`). The system SHALL refuse to start if this variable is missing or invalid.

#### Scenario: Missing master key
- **WHEN** the system starts without `ARX_MASTER_KEY` set
- **THEN** the system SHALL fail to start with a clear error message

#### Scenario: Master key loaded successfully
- **WHEN** the system starts with a valid `ARX_MASTER_KEY`
- **THEN** the system SHALL initialize the LocalKeyManager and proceed with startup

### Requirement: Ed25519 webhook signature verification
The system SHALL verify Ed25519 signatures on all inbound merchant webhooks. The signature SHALL cover the HTTP method, path, body, timestamp, and nonce. During key rotation grace periods, the system SHALL try both current and previous merchant public keys.

#### Scenario: Signature verified with current key
- **WHEN** a webhook arrives signed with the merchant's current Ed25519 key
- **THEN** the system SHALL verify successfully and process the webhook

#### Scenario: Signature verified with old key during grace period
- **WHEN** a webhook arrives signed with the merchant's previous key and the grace period has not expired
- **THEN** the system SHALL verify successfully

#### Scenario: Signature with old key after grace period
- **WHEN** a webhook arrives signed with the merchant's previous key and the grace period has expired
- **THEN** the system SHALL reject with 401 and `"invalid_signature"`

### Requirement: Ed25519 outbound request signing
The system SHALL sign every outbound proxied request with the tenant's Arx signing key (Ed25519). The signing key SHALL be stored encrypted with the tenant's DEK.

#### Scenario: Signing key decrypted for use
- **WHEN** the system needs to sign an outbound request for tenant "acme"
- **THEN** the system SHALL decrypt the signing private key using the tenant's DEK and the master key, sign the request, and not persist the decrypted key

### Requirement: Key rotation with grace period
The system SHALL support rotating both merchant public keys and Arx signing keypairs. During rotation, the old key SHALL remain valid for a configurable grace period (default 24 hours). Key history SHALL be recorded in the tenant_keys table.

#### Scenario: Arx signing key rotation
- **WHEN** an admin triggers signing key rotation for a tenant
- **THEN** the system SHALL generate a new keypair, store the old key with an expiry timestamp, and begin signing with the new key immediately

#### Scenario: Key history recorded
- **WHEN** any key rotation occurs
- **THEN** the system SHALL insert a record in tenant_keys with `active_from` and `active_until` timestamps
