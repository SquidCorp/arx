package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/fambr/arx/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) (*cache.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client, err := cache.Connect(context.Background(), "redis://"+mr.Addr())
	require.NoError(t, err)
	return client, mr
}

func sampleSession() *cache.SessionData {
	return &cache.SessionData{
		TenantID:    "tenant-abc",
		MerchantSID: "merchant-123",
		UserID:      "user-456",
		Status:      "active",
		Scopes:      []string{"cart:read", "cart:write"},
		ExpiresAt:   time.Now().Add(30 * time.Minute),
	}
}

func TestSessionCache_SetAndGet(t *testing.T) {
	client, _ := newTestClient(t)
	sc := cache.NewSessionCache(client)
	ctx := context.Background()

	session := sampleSession()
	err := sc.Set(ctx, "sess-1", session, 10*time.Minute)
	require.NoError(t, err)

	got, err := sc.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, session.TenantID, got.TenantID)
	assert.Equal(t, session.MerchantSID, got.MerchantSID)
	assert.Equal(t, session.UserID, got.UserID)
	assert.Equal(t, session.Status, got.Status)
	assert.Equal(t, session.Scopes, got.Scopes)
	assert.WithinDuration(t, session.ExpiresAt, got.ExpiresAt, time.Second)
}

func TestSessionCache_GetMiss(t *testing.T) {
	client, _ := newTestClient(t)
	sc := cache.NewSessionCache(client)
	ctx := context.Background()

	got, err := sc.Get(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got, "cache miss should return nil without error")
}

func TestSessionCache_Delete(t *testing.T) {
	client, _ := newTestClient(t)
	sc := cache.NewSessionCache(client)
	ctx := context.Background()

	session := sampleSession()
	require.NoError(t, sc.Set(ctx, "sess-1", session, 10*time.Minute))

	err := sc.Delete(ctx, "sess-1")
	require.NoError(t, err)

	got, err := sc.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSessionCache_TTLExpiry(t *testing.T) {
	client, mr := newTestClient(t)
	sc := cache.NewSessionCache(client)
	ctx := context.Background()

	session := sampleSession()
	require.NoError(t, sc.Set(ctx, "sess-1", session, 1*time.Second))

	// Fast-forward miniredis time
	mr.FastForward(2 * time.Second)

	got, err := sc.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.Nil(t, got, "expired key should return nil")
}

func TestSessionCache_Update(t *testing.T) {
	client, _ := newTestClient(t)
	sc := cache.NewSessionCache(client)
	ctx := context.Background()

	session := sampleSession()
	require.NoError(t, sc.Set(ctx, "sess-1", session, 10*time.Minute))

	// Update status
	session.Status = "suspended"
	require.NoError(t, sc.Set(ctx, "sess-1", session, 10*time.Minute))

	got, err := sc.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "suspended", got.Status)
}

func TestSessionCache_CircuitBreakerFallback(t *testing.T) {
	client, mr := newTestClient(t)
	sc := cache.NewSessionCache(client)
	ctx := context.Background()

	// Store a session
	session := sampleSession()
	require.NoError(t, sc.Set(ctx, "sess-1", session, 10*time.Minute))

	// Close Redis
	mr.Close()

	// Get should return error (not ErrCircuitOpen yet, just connection error)
	_, err := sc.Get(ctx, "sess-1")
	require.Error(t, err)
}
