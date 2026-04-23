-- +goose Up
CREATE TABLE tenant_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    key_type        TEXT NOT NULL CHECK (key_type IN ('arx_signing', 'merchant_public')),
    public_key      BYTEA NOT NULL,
    private_key_enc BYTEA,
    active_from     TIMESTAMPTZ NOT NULL DEFAULT now(),
    active_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tenant_keys_tenant_type ON tenant_keys (tenant_id, key_type);
CREATE INDEX idx_tenant_keys_active ON tenant_keys (tenant_id, key_type, active_from, active_until);

-- +goose Down
DROP TABLE IF EXISTS tenant_keys;
