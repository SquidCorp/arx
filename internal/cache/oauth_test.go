package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleOAuthFlow() *cache.OAuthFlowData {
	return &cache.OAuthFlowData{
		TenantID:            "tenant-abc",
		ClientID:            "client-xyz",
		RedirectURI:         "https://llm.example.com/callback",
		State:               "random-state-value",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		Status:              "pending",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}
}

func TestOAuthCache_SetAndGet(t *testing.T) {
	client, _ := newTestClient(t)
	oc := cache.NewOAuthCache(client)
	ctx := context.Background()

	flow := sampleOAuthFlow()
	err := oc.Set(ctx, "flow-uuid-1", flow)
	require.NoError(t, err)

	got, err := oc.Get(ctx, "flow-uuid-1")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, flow.TenantID, got.TenantID)
	assert.Equal(t, flow.ClientID, got.ClientID)
	assert.Equal(t, flow.RedirectURI, got.RedirectURI)
	assert.Equal(t, flow.State, got.State)
	assert.Equal(t, flow.CodeChallenge, got.CodeChallenge)
	assert.Equal(t, flow.CodeChallengeMethod, got.CodeChallengeMethod)
	assert.Equal(t, flow.Status, got.Status)
	assert.WithinDuration(t, flow.ExpiresAt, got.ExpiresAt, time.Second)
}

func TestOAuthCache_GetMiss(t *testing.T) {
	client, _ := newTestClient(t)
	oc := cache.NewOAuthCache(client)
	ctx := context.Background()

	got, err := oc.Get(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestOAuthCache_TTLExpiry(t *testing.T) {
	client, mr := newTestClient(t)
	oc := cache.NewOAuthCache(client)
	ctx := context.Background()

	flow := sampleOAuthFlow()
	require.NoError(t, oc.Set(ctx, "flow-uuid-1", flow))

	// Fast-forward past 10-min TTL
	mr.FastForward(11 * time.Minute)

	got, err := oc.Get(ctx, "flow-uuid-1")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestOAuthCache_Update(t *testing.T) {
	client, _ := newTestClient(t)
	oc := cache.NewOAuthCache(client)
	ctx := context.Background()

	flow := sampleOAuthFlow()
	require.NoError(t, oc.Set(ctx, "flow-uuid-1", flow))

	// Update status and add auth code + session
	flow.Status = "connected"
	flow.AuthCode = "auth-code-123"
	flow.SessionID = "session-uuid-456"
	require.NoError(t, oc.Set(ctx, "flow-uuid-1", flow))

	got, err := oc.Get(ctx, "flow-uuid-1")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "connected", got.Status)
	assert.Equal(t, "auth-code-123", got.AuthCode)
	assert.Equal(t, "session-uuid-456", got.SessionID)
}

func TestOAuthCache_Delete(t *testing.T) {
	client, _ := newTestClient(t)
	oc := cache.NewOAuthCache(client)
	ctx := context.Background()

	flow := sampleOAuthFlow()
	require.NoError(t, oc.Set(ctx, "flow-uuid-1", flow))

	err := oc.Delete(ctx, "flow-uuid-1")
	require.NoError(t, err)

	got, err := oc.Get(ctx, "flow-uuid-1")
	require.NoError(t, err)
	assert.Nil(t, got)
}
