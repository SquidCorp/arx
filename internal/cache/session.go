package cache

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionData holds the cached fields of a session.
type SessionData struct {
	// TenantID is the owning tenant.
	TenantID string
	// MerchantSID is the merchant-assigned session identifier.
	MerchantSID string
	// UserID is the authenticated user.
	UserID string
	// Status is the session state (active, expired, revoked, suspended).
	Status string
	// Scopes is the list of granted scopes.
	Scopes []string
	// ExpiresAt is the session expiration time.
	ExpiresAt time.Time
}

// sessionKey returns the Redis key for a session hash.
func sessionKey(id string) string {
	return "session:" + id
}

// SessionCache provides write-through and read-through caching for sessions.
type SessionCache struct {
	client *Client
}

// NewSessionCache creates a SessionCache backed by the given Client.
func NewSessionCache(c *Client) *SessionCache {
	return &SessionCache{client: c}
}

// Set writes a session to Redis as a HASH with the given TTL.
// This is the write-through path — call on create and update.
func (sc *SessionCache) Set(ctx context.Context, id string, s *SessionData, ttl time.Duration) error {
	key := sessionKey(id)
	fields := map[string]string{
		"tenant_id":    s.TenantID,
		"merchant_sid": s.MerchantSID,
		"user_id":      s.UserID,
		"status":       s.Status,
		"scopes":       strings.Join(s.Scopes, ","),
		"expires_at":   s.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}

	return sc.client.Do(ctx, func(rdb *redis.Client) error {
		pipe := rdb.Pipeline()
		pipe.HSet(ctx, key, fields)
		pipe.Expire(ctx, key, ttl)
		_, err := pipe.Exec(ctx)
		return err
	})
}

// Get reads a session from Redis. Returns nil, nil on cache miss.
// This is the read-through entry point — callers fall back to Postgres on nil.
func (sc *SessionCache) Get(ctx context.Context, id string) (*SessionData, error) {
	key := sessionKey(id)
	var result map[string]string

	err := sc.client.Do(ctx, func(rdb *redis.Client) error {
		var cmdErr error
		result, cmdErr = rdb.HGetAll(ctx, key).Result()
		return cmdErr
	})
	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, nil
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, result["expires_at"])
	if err != nil {
		return nil, err
	}

	var scopes []string
	if s := result["scopes"]; s != "" {
		scopes = strings.Split(s, ",")
	}

	return &SessionData{
		TenantID:    result["tenant_id"],
		MerchantSID: result["merchant_sid"],
		UserID:      result["user_id"],
		Status:      result["status"],
		Scopes:      scopes,
		ExpiresAt:   expiresAt,
	}, nil
}

// Delete removes a session from the cache. Used on revocation.
func (sc *SessionCache) Delete(ctx context.Context, id string) error {
	return sc.client.Do(ctx, func(rdb *redis.Client) error {
		return rdb.Del(ctx, sessionKey(id)).Err()
	})
}

// ErrCacheMiss can be checked by callers to distinguish a miss from an error.
// For SessionCache, a miss returns (nil, nil), but this sentinel is available
// for callers who prefer explicit error checking.
var ErrCacheMiss = errors.New("cache: miss")
