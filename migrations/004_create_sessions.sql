-- +goose Up
CREATE TABLE sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id),
    merchant_sid        TEXT NOT NULL,
    user_id             TEXT,
    is_anonymous        BOOLEAN NOT NULL DEFAULT false,
    status              TEXT NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'expired', 'revoked', 'suspended')),
    scopes              TEXT[] NOT NULL DEFAULT '{}',
    claims_enc          BYTEA,
    bind_code           TEXT,
    bind_code_expires_at TIMESTAMPTZ,
    refresh_count       INT NOT NULL DEFAULT 0,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_tenant_status ON sessions (tenant_id, status);
CREATE INDEX idx_sessions_merchant_sid ON sessions (merchant_sid);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at) WHERE status = 'active';

-- +goose Down
DROP TABLE IF EXISTS sessions;
