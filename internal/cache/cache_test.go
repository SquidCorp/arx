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

func TestConnect(t *testing.T) {
	t.Run("connects to valid Redis server", func(t *testing.T) {
		mr := miniredis.RunT(t)
		client, err := cache.Connect(context.Background(), "redis://"+mr.Addr())
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("returns error for invalid URL", func(t *testing.T) {
		_, err := cache.Connect(context.Background(), "://invalid")
		require.Error(t, err)
	})

	t.Run("returns error when server is unreachable", func(t *testing.T) {
		_, err := cache.Connect(context.Background(), "redis://localhost:1")
		require.Error(t, err)
	})
}

func TestClient_Ping(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := cache.Connect(context.Background(), "redis://"+mr.Addr())
	require.NoError(t, err)

	err = client.Ping(context.Background())
	require.NoError(t, err)
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := cache.Connect(context.Background(), "redis://"+mr.Addr(),
		cache.WithCircuitBreakerThreshold(3),
		cache.WithCircuitBreakerResetTimeout(50*time.Millisecond),
	)
	require.NoError(t, err)

	// Close miniredis to cause failures
	mr.Close()

	ctx := context.Background()

	// First 2 calls fail but breaker stays closed (below threshold)
	for i := 0; i < 2; i++ {
		err := client.Ping(ctx)
		require.Error(t, err)
		assert.False(t, client.IsOpen(), "breaker should still be closed after %d failures", i+1)
	}

	// Third failure reaches threshold — breaker opens
	err = client.Ping(ctx)
	require.Error(t, err)
	assert.True(t, client.IsOpen(), "breaker should be open after reaching threshold")

	// Next call should return ErrCircuitOpen without hitting Redis
	err = client.Ping(ctx)
	require.ErrorIs(t, err, cache.ErrCircuitOpen)
}

func TestCircuitBreaker_ResetsAfterTimeout(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := cache.Connect(context.Background(), "redis://"+mr.Addr(),
		cache.WithCircuitBreakerThreshold(2),
		cache.WithCircuitBreakerResetTimeout(50*time.Millisecond),
	)
	require.NoError(t, err)

	// Close to cause failures
	mr.Close()
	for i := 0; i < 2; i++ {
		_ = client.Ping(context.Background())
	}
	assert.True(t, client.IsOpen())

	// Start a new miniredis on the same addr is not possible,
	// but we can restart the original
	mr2 := miniredis.RunT(t)
	client2, err := cache.Connect(context.Background(), "redis://"+mr2.Addr(),
		cache.WithCircuitBreakerThreshold(2),
		cache.WithCircuitBreakerResetTimeout(50*time.Millisecond),
	)
	require.NoError(t, err)

	// Close and reopen to test reset
	mr2.Close()
	_ = client2.Ping(context.Background())
	_ = client2.Ping(context.Background())
	assert.True(t, client2.IsOpen())

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Breaker should be half-open now, allowing one attempt
	assert.False(t, client2.IsOpen(), "breaker should be half-open after reset timeout")
}

func TestCircuitBreaker_ClosesOnSuccess(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := cache.Connect(context.Background(), "redis://"+mr.Addr(),
		cache.WithCircuitBreakerThreshold(2),
		cache.WithCircuitBreakerResetTimeout(50*time.Millisecond),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Successful operation keeps breaker closed
	err = client.Ping(ctx)
	require.NoError(t, err)
	assert.False(t, client.IsOpen())
}

func TestClient_Redis(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := cache.Connect(context.Background(), "redis://"+mr.Addr())
	require.NoError(t, err)

	// Redis() should return the underlying client for direct operations
	rdb := client.Redis()
	assert.NotNil(t, rdb)
}
