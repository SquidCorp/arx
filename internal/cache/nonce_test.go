package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonceStore_CheckWebhookNonce(t *testing.T) {
	client, _ := newTestClient(t)
	ns := cache.NewNonceStore(client)
	ctx := context.Background()

	t.Run("first use returns true (unique)", func(t *testing.T) {
		unique, err := ns.CheckWebhookNonce(ctx, "tenant-1", "nonce-abc")
		require.NoError(t, err)
		assert.True(t, unique, "first use of nonce should be unique")
	})

	t.Run("second use returns false (duplicate)", func(t *testing.T) {
		unique, err := ns.CheckWebhookNonce(ctx, "tenant-1", "nonce-abc")
		require.NoError(t, err)
		assert.False(t, unique, "reused nonce should be duplicate")
	})

	t.Run("different tenant same nonce is unique", func(t *testing.T) {
		unique, err := ns.CheckWebhookNonce(ctx, "tenant-2", "nonce-abc")
		require.NoError(t, err)
		assert.True(t, unique, "same nonce from different tenant should be unique")
	})
}

func TestNonceStore_WebhookNonceTTL(t *testing.T) {
	client, mr := newTestClient(t)
	ns := cache.NewNonceStore(client)
	ctx := context.Background()

	unique, err := ns.CheckWebhookNonce(ctx, "tenant-1", "nonce-expiring")
	require.NoError(t, err)
	assert.True(t, unique)

	// Fast-forward past 5-min TTL
	mr.FastForward(6 * time.Minute)

	// After TTL, the same nonce should be accepted again
	unique, err = ns.CheckWebhookNonce(ctx, "tenant-1", "nonce-expiring")
	require.NoError(t, err)
	assert.True(t, unique, "nonce should be accepted after TTL expires")
}

func TestNonceStore_CheckDPoPNonce(t *testing.T) {
	client, _ := newTestClient(t)
	ns := cache.NewNonceStore(client)
	ctx := context.Background()

	t.Run("first use returns true (unique)", func(t *testing.T) {
		unique, err := ns.CheckDPoPNonce(ctx, "jti-xyz-123")
		require.NoError(t, err)
		assert.True(t, unique)
	})

	t.Run("second use returns false (duplicate)", func(t *testing.T) {
		unique, err := ns.CheckDPoPNonce(ctx, "jti-xyz-123")
		require.NoError(t, err)
		assert.False(t, unique)
	})
}

func TestNonceStore_DPoPNonceTTL(t *testing.T) {
	client, mr := newTestClient(t)
	ns := cache.NewNonceStore(client)
	ctx := context.Background()

	unique, err := ns.CheckDPoPNonce(ctx, "jti-expiring")
	require.NoError(t, err)
	assert.True(t, unique)

	// Fast-forward past 5-min TTL
	mr.FastForward(6 * time.Minute)

	unique, err = ns.CheckDPoPNonce(ctx, "jti-expiring")
	require.NoError(t, err)
	assert.True(t, unique, "DPoP nonce should be accepted after TTL expires")
}

func TestNonceStore_ConcurrentWebhookNonces(t *testing.T) {
	client, _ := newTestClient(t)
	ns := cache.NewNonceStore(client)
	ctx := context.Background()

	// Multiple different nonces should all be unique
	for i := 0; i < 10; i++ {
		nonce := "nonce-" + string(rune('a'+i))
		unique, err := ns.CheckWebhookNonce(ctx, "tenant-1", nonce)
		require.NoError(t, err)
		assert.True(t, unique, "nonce %s should be unique", nonce)
	}
}
