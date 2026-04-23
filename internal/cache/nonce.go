package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const nonceTTL = 5 * time.Minute

// NonceStore provides atomic nonce deduplication using Redis SET NX.
type NonceStore struct {
	client *Client
}

// NewNonceStore creates a NonceStore backed by the given Client.
func NewNonceStore(c *Client) *NonceStore {
	return &NonceStore{client: c}
}

// webhookNonceKey returns the Redis key for a webhook nonce.
func webhookNonceKey(tenantID, nonce string) string {
	return "nonce:" + tenantID + ":" + nonce
}

// dpopNonceKey returns the Redis key for a DPoP JTI nonce.
func dpopNonceKey(jti string) string {
	return "nonce:dpop:" + jti
}

// CheckWebhookNonce atomically checks and records a webhook nonce.
// Returns true if the nonce is unique (first use), false if it's a duplicate.
// The nonce expires after 5 minutes.
func (ns *NonceStore) CheckWebhookNonce(ctx context.Context, tenantID, nonce string) (bool, error) {
	return ns.checkNonce(ctx, webhookNonceKey(tenantID, nonce))
}

// CheckDPoPNonce atomically checks and records a DPoP JTI nonce.
// Returns true if the JTI is unique (first use), false if it's a duplicate.
// The nonce expires after 5 minutes.
func (ns *NonceStore) CheckDPoPNonce(ctx context.Context, jti string) (bool, error) {
	return ns.checkNonce(ctx, dpopNonceKey(jti))
}

// checkNonce implements the SET NX EX pattern for atomic nonce deduplication.
func (ns *NonceStore) checkNonce(ctx context.Context, key string) (bool, error) {
	var unique bool

	err := ns.client.Do(ctx, func(rdb *redis.Client) error {
		result, cmdErr := rdb.SetArgs(ctx, key, "1", redis.SetArgs{
			Mode: "NX",
			TTL:  nonceTTL,
		}).Result()
		if cmdErr == redis.Nil {
			// Key already exists — nonce is a duplicate
			unique = false
			return nil
		}
		if cmdErr != nil {
			return cmdErr
		}
		unique = result == "OK"
		if cmdErr != nil {
			return cmdErr
		}
		return nil
	})

	return unique, err
}
