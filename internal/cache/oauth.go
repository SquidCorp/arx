package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const oauthTTL = 10 * time.Minute

// OAuthFlowData holds the cached fields of a pending OAuth flow.
type OAuthFlowData struct {
	// TenantID is the owning tenant.
	TenantID string
	// ClientID is the OAuth client identifier.
	ClientID string
	// RedirectURI is the LLM's callback URL.
	RedirectURI string
	// State is the OAuth state parameter for CSRF protection.
	State string
	// CodeChallenge is the PKCE challenge.
	CodeChallenge string
	// CodeChallengeMethod is the PKCE method (always "S256").
	CodeChallengeMethod string
	// AuthCode is set after the merchant completes authentication.
	AuthCode string
	// SessionID is set after the session is created via webhook.
	SessionID string
	// Status is the flow state (pending, connected, expired, consumed).
	Status string
	// ExpiresAt is the flow expiration time.
	ExpiresAt time.Time
}

// oauthKey returns the Redis key for an OAuth flow HASH.
func oauthKey(uuid string) string {
	return "oauth:" + uuid
}

// OAuthCache provides caching for pending OAuth flows with a 10-minute TTL.
type OAuthCache struct {
	client *Client
}

// NewOAuthCache creates an OAuthCache backed by the given Client.
func NewOAuthCache(c *Client) *OAuthCache {
	return &OAuthCache{client: c}
}

// Set writes an OAuth flow to Redis as a HASH with a 10-minute TTL.
func (oc *OAuthCache) Set(ctx context.Context, uuid string, f *OAuthFlowData) error {
	key := oauthKey(uuid)

	fields := map[string]string{
		"tenant_id":             f.TenantID,
		"client_id":             f.ClientID,
		"redirect_uri":          f.RedirectURI,
		"state":                 f.State,
		"code_challenge":        f.CodeChallenge,
		"code_challenge_method": f.CodeChallengeMethod,
		"auth_code":             f.AuthCode,
		"session_id":            f.SessionID,
		"status":                f.Status,
		"expires_at":            f.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}

	return oc.client.Do(ctx, func(rdb *redis.Client) error {
		pipe := rdb.Pipeline()
		pipe.HSet(ctx, key, fields)
		pipe.Expire(ctx, key, oauthTTL)
		_, err := pipe.Exec(ctx)
		return err
	})
}

// Get reads an OAuth flow from Redis. Returns nil, nil on cache miss.
func (oc *OAuthCache) Get(ctx context.Context, uuid string) (*OAuthFlowData, error) {
	key := oauthKey(uuid)
	var result map[string]string

	err := oc.client.Do(ctx, func(rdb *redis.Client) error {
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

	return &OAuthFlowData{
		TenantID:            result["tenant_id"],
		ClientID:            result["client_id"],
		RedirectURI:         result["redirect_uri"],
		State:               result["state"],
		CodeChallenge:       result["code_challenge"],
		CodeChallengeMethod: result["code_challenge_method"],
		AuthCode:            result["auth_code"],
		SessionID:           result["session_id"],
		Status:              result["status"],
		ExpiresAt:           expiresAt,
	}, nil
}

// Delete removes an OAuth flow from the cache.
func (oc *OAuthCache) Delete(ctx context.Context, uuid string) error {
	return oc.client.Do(ctx, func(rdb *redis.Client) error {
		return rdb.Del(ctx, oauthKey(uuid)).Err()
	})
}
