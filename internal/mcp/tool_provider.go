package mcp

import (
	"context"
	"fmt"

	"github.com/fambr/arx/internal/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBToolProvider reads tenant tools directly from the database.
type DBToolProvider struct {
	pool *pgxpool.Pool
}

// NewDBToolProvider creates a ToolProvider backed by a Postgres connection pool.
func NewDBToolProvider(pool *pgxpool.Pool) *DBToolProvider {
	return &DBToolProvider{pool: pool}
}

// TenantTools returns all enabled tools for a tenant as scope.Tool entries.
func (p *DBToolProvider) TenantTools(ctx context.Context, tenantID string) ([]scope.Tool, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT name, catalog_type, required_scopes
		FROM tools
		WHERE tenant_id = $1 AND enabled = true
		ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant tools: %w", err)
	}
	defer rows.Close()

	var tools []scope.Tool
	for rows.Next() {
		var t scope.Tool
		if err := rows.Scan(&t.Name, &t.CatalogType, &t.RequiredScopes); err != nil {
			return nil, fmt.Errorf("scan tool row: %w", err)
		}
		tools = append(tools, t)
	}
	return tools, rows.Err()
}
