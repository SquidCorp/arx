package cache

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const toolTTL = 5 * time.Minute

// ToolData holds the cached fields of a tool configuration.
type ToolData struct {
	// ID is the tool's UUID.
	ID string
	// CatalogType is the standard catalog type (e.g., "cart.add").
	CatalogType string
	// UpstreamURL is the merchant endpoint to proxy to.
	UpstreamURL string
	// UpstreamMethod is the HTTP method for the upstream call.
	UpstreamMethod string
	// ParamMapping is the JSON mapping from standard params to upstream fields.
	ParamMapping string
	// RequiredScopes lists scopes needed to call this tool.
	RequiredScopes []string
	// TimeoutMs is the upstream call timeout in milliseconds.
	TimeoutMs int
	// Enabled indicates whether the tool is active.
	Enabled bool
}

// toolKey returns the Redis key for a tool HASH.
func toolKey(tenantID, name string) string {
	return "tool:" + tenantID + ":" + name
}

// tenantToolsKey returns the Redis key for the tenant's tool name SET.
func tenantToolsKey(tenantID string) string {
	return "tenant:" + tenantID + ":tools"
}

// ToolCache provides caching for tool configurations with a 5-minute TTL.
type ToolCache struct {
	client *Client
}

// NewToolCache creates a ToolCache backed by the given Client.
func NewToolCache(c *Client) *ToolCache {
	return &ToolCache{client: c}
}

// Set writes a tool to Redis as a HASH and adds its name to the tenant's tool SET.
func (tc *ToolCache) Set(ctx context.Context, tenantID, name string, t *ToolData) error {
	tk := toolKey(tenantID, name)
	ttk := tenantToolsKey(tenantID)

	fields := map[string]string{
		"id":              t.ID,
		"catalog_type":    t.CatalogType,
		"upstream_url":    t.UpstreamURL,
		"upstream_method": t.UpstreamMethod,
		"param_mapping":   t.ParamMapping,
		"required_scopes": strings.Join(t.RequiredScopes, ","),
		"timeout_ms":      strconv.Itoa(t.TimeoutMs),
		"enabled":         strconv.FormatBool(t.Enabled),
	}

	return tc.client.Do(ctx, func(rdb *redis.Client) error {
		pipe := rdb.Pipeline()
		pipe.HSet(ctx, tk, fields)
		pipe.Expire(ctx, tk, toolTTL)
		pipe.SAdd(ctx, ttk, name)
		pipe.Expire(ctx, ttk, toolTTL)
		_, err := pipe.Exec(ctx)
		return err
	})
}

// Get reads a tool from Redis. Returns nil, nil on cache miss.
func (tc *ToolCache) Get(ctx context.Context, tenantID, name string) (*ToolData, error) {
	tk := toolKey(tenantID, name)
	var result map[string]string

	err := tc.client.Do(ctx, func(rdb *redis.Client) error {
		var cmdErr error
		result, cmdErr = rdb.HGetAll(ctx, tk).Result()
		return cmdErr
	})
	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, nil
	}

	timeoutMs, _ := strconv.Atoi(result["timeout_ms"])
	enabled, _ := strconv.ParseBool(result["enabled"])

	var scopes []string
	if s := result["required_scopes"]; s != "" {
		scopes = strings.Split(s, ",")
	}

	return &ToolData{
		ID:             result["id"],
		CatalogType:    result["catalog_type"],
		UpstreamURL:    result["upstream_url"],
		UpstreamMethod: result["upstream_method"],
		ParamMapping:   result["param_mapping"],
		RequiredScopes: scopes,
		TimeoutMs:      timeoutMs,
		Enabled:        enabled,
	}, nil
}

// ListNames returns all cached tool names for a tenant.
func (tc *ToolCache) ListNames(ctx context.Context, tenantID string) ([]string, error) {
	ttk := tenantToolsKey(tenantID)
	var names []string

	err := tc.client.Do(ctx, func(rdb *redis.Client) error {
		var cmdErr error
		names, cmdErr = rdb.SMembers(ctx, ttk).Result()
		return cmdErr
	})
	if err != nil {
		return nil, err
	}

	return names, nil
}

// Delete removes a tool from the cache and from the tenant's tool SET.
func (tc *ToolCache) Delete(ctx context.Context, tenantID, name string) error {
	tk := toolKey(tenantID, name)
	ttk := tenantToolsKey(tenantID)

	return tc.client.Do(ctx, func(rdb *redis.Client) error {
		pipe := rdb.Pipeline()
		pipe.Del(ctx, tk)
		pipe.SRem(ctx, ttk, name)
		_, err := pipe.Exec(ctx)
		return err
	})
}

// InvalidateTenant removes all cached tools for a tenant.
func (tc *ToolCache) InvalidateTenant(ctx context.Context, tenantID string) error {
	ttk := tenantToolsKey(tenantID)

	return tc.client.Do(ctx, func(rdb *redis.Client) error {
		names, err := rdb.SMembers(ctx, ttk).Result()
		if err != nil {
			return err
		}

		if len(names) == 0 {
			return nil
		}

		pipe := rdb.Pipeline()
		for _, name := range names {
			pipe.Del(ctx, toolKey(tenantID, name))
		}
		pipe.Del(ctx, ttk)
		_, err = pipe.Exec(ctx)
		return err
	})
}
