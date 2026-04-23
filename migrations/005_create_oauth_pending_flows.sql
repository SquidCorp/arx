-- +goose Up
CREATE TABLE oauth_pending_flows (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    uuid                  TEXT NOT NULL,
    tenant_id             UUID NOT NULL REFERENCES tenants(id),
    client_id             TEXT NOT NULL,
    redirect_uri          TEXT NOT NULL,
    state                 TEXT NOT NULL,
    code_challenge        TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL DEFAULT 'S256',
    auth_code             TEXT,
    session_id            UUID REFERENCES sessions(id),
    status                TEXT NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'connected', 'expired', 'consumed')),
    expires_at            TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_oauth_pending_flows_uuid ON oauth_pending_flows (uuid);
CREATE INDEX idx_oauth_pending_flows_expires_at ON oauth_pending_flows (expires_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS oauth_pending_flows;
