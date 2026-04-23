-- +goose Up
CREATE TABLE tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deactivated')),

    -- Merchant's Ed25519 public key for webhook signature verification
    merchant_public_key          BYTEA NOT NULL,
    merchant_public_key_previous BYTEA,
    merchant_key_grace_until     TIMESTAMPTZ,

    -- Arx Ed25519 signing keypair (private key encrypted with tenant DEK)
    arx_signing_public_key      BYTEA NOT NULL,
    arx_signing_private_key_enc BYTEA NOT NULL,

    -- Per-tenant data encryption key (encrypted with master key)
    dek_enc BYTEA NOT NULL,

    -- OAuth configuration
    oauth_redirect_uris TEXT[] NOT NULL DEFAULT '{}',
    merchant_login_url  TEXT NOT NULL DEFAULT '',

    -- Session policy
    session_max_expiry INTERVAL NOT NULL DEFAULT '1 hour',
    max_refreshes      INT NOT NULL DEFAULT 5,

    -- Admin API authentication
    api_key_hash TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS tenants;
