-- +goose Up
CREATE TABLE tools (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    name          TEXT NOT NULL,
    catalog_type  TEXT NOT NULL,
    upstream_url  TEXT NOT NULL,
    upstream_method TEXT NOT NULL DEFAULT 'POST',
    param_mapping JSONB NOT NULL DEFAULT '{}',
    required_scopes TEXT[] NOT NULL DEFAULT '{}',
    timeout_ms    INT NOT NULL DEFAULT 5000,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, name)
);

CREATE INDEX idx_tools_tenant_id ON tools (tenant_id);

-- +goose Down
DROP TABLE IF EXISTS tools;
