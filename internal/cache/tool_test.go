package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleTool() *cache.ToolData {
	return &cache.ToolData{
		ID:             "tool-uuid-1",
		CatalogType:    "cart.add",
		UpstreamURL:    "https://api.merchant.com/cart/add",
		UpstreamMethod: "POST",
		ParamMapping:   `{"product_id":"body.item_id","quantity":"body.qty"}`,
		RequiredScopes: []string{"cart:write"},
		TimeoutMs:      5000,
		Enabled:        true,
	}
}

func TestToolCache_SetAndGet(t *testing.T) {
	client, _ := newTestClient(t)
	tc := cache.NewToolCache(client)
	ctx := context.Background()

	tool := sampleTool()
	err := tc.Set(ctx, "tenant-1", "cart.add", tool)
	require.NoError(t, err)

	got, err := tc.Get(ctx, "tenant-1", "cart.add")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, tool.ID, got.ID)
	assert.Equal(t, tool.CatalogType, got.CatalogType)
	assert.Equal(t, tool.UpstreamURL, got.UpstreamURL)
	assert.Equal(t, tool.UpstreamMethod, got.UpstreamMethod)
	assert.Equal(t, tool.ParamMapping, got.ParamMapping)
	assert.Equal(t, tool.RequiredScopes, got.RequiredScopes)
	assert.Equal(t, tool.TimeoutMs, got.TimeoutMs)
	assert.Equal(t, tool.Enabled, got.Enabled)
}

func TestToolCache_GetMiss(t *testing.T) {
	client, _ := newTestClient(t)
	tc := cache.NewToolCache(client)
	ctx := context.Background()

	got, err := tc.Get(ctx, "tenant-1", "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestToolCache_TTLExpiry(t *testing.T) {
	client, mr := newTestClient(t)
	tc := cache.NewToolCache(client)
	ctx := context.Background()

	tool := sampleTool()
	require.NoError(t, tc.Set(ctx, "tenant-1", "cart.add", tool))

	// Fast-forward past 5-min TTL
	mr.FastForward(6 * time.Minute)

	got, err := tc.Get(ctx, "tenant-1", "cart.add")
	require.NoError(t, err)
	assert.Nil(t, got, "expired tool should return nil")
}

func TestToolCache_TenantToolsList(t *testing.T) {
	client, _ := newTestClient(t)
	tc := cache.NewToolCache(client)
	ctx := context.Background()

	// Add two tools for tenant-1
	tool1 := sampleTool()
	tool2 := &cache.ToolData{
		ID:             "tool-uuid-2",
		CatalogType:    "cart.view",
		UpstreamURL:    "https://api.merchant.com/cart",
		UpstreamMethod: "GET",
		ParamMapping:   `{}`,
		RequiredScopes: []string{"cart:read"},
		TimeoutMs:      3000,
		Enabled:        true,
	}

	require.NoError(t, tc.Set(ctx, "tenant-1", "cart.add", tool1))
	require.NoError(t, tc.Set(ctx, "tenant-1", "cart.view", tool2))

	names, err := tc.ListNames(ctx, "tenant-1")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cart.add", "cart.view"}, names)
}

func TestToolCache_TenantToolsListMiss(t *testing.T) {
	client, _ := newTestClient(t)
	tc := cache.NewToolCache(client)
	ctx := context.Background()

	names, err := tc.ListNames(ctx, "no-tenant")
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestToolCache_Delete(t *testing.T) {
	client, _ := newTestClient(t)
	tc := cache.NewToolCache(client)
	ctx := context.Background()

	tool := sampleTool()
	require.NoError(t, tc.Set(ctx, "tenant-1", "cart.add", tool))

	err := tc.Delete(ctx, "tenant-1", "cart.add")
	require.NoError(t, err)

	got, err := tc.Get(ctx, "tenant-1", "cart.add")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Should also be removed from the tenant tools set
	names, err := tc.ListNames(ctx, "tenant-1")
	require.NoError(t, err)
	assert.NotContains(t, names, "cart.add")
}

func TestToolCache_InvalidateTenant(t *testing.T) {
	client, _ := newTestClient(t)
	tc := cache.NewToolCache(client)
	ctx := context.Background()

	tool := sampleTool()
	require.NoError(t, tc.Set(ctx, "tenant-1", "cart.add", tool))
	require.NoError(t, tc.Set(ctx, "tenant-1", "cart.view", &cache.ToolData{
		ID: "tool-2", CatalogType: "cart.view", Enabled: true,
	}))

	err := tc.InvalidateTenant(ctx, "tenant-1")
	require.NoError(t, err)

	names, err := tc.ListNames(ctx, "tenant-1")
	require.NoError(t, err)
	assert.Empty(t, names)
}
