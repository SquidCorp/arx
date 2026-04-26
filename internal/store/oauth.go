package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fambr/arx/internal/oauth"
	"github.com/fambr/arx/internal/session"
	"github.com/jackc/pgx/v5"
)

// CreatePendingFlow inserts a new pending OAuth flow.
func (s *Store) CreatePendingFlow(ctx context.Context, flow *oauth.PendingFlow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO oauth_pending_flows (
			uuid, tenant_id, client_id, redirect_uri, state,
			code_challenge, code_challenge_method, auth_code, status, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, flow.UUID, flow.TenantID, flow.ClientID, flow.RedirectURI, flow.State,
		flow.CodeChallenge, flow.CodeChallengeMethod, flow.AuthCode, flow.Status, flow.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert pending flow: %w", err)
	}
	return nil
}

// GetPendingFlow retrieves a pending flow by UUID.
func (s *Store) GetPendingFlow(ctx context.Context, uuid string) (*oauth.PendingFlow, error) {
	var flow oauth.PendingFlow
	err := s.pool.QueryRow(ctx, `
		SELECT uuid, tenant_id::text, client_id, redirect_uri, state,
			   code_challenge, code_challenge_method, auth_code,
			   COALESCE(session_id::text, ''), status, expires_at
		FROM oauth_pending_flows WHERE uuid = $1
	`, uuid).Scan(
		&flow.UUID, &flow.TenantID, &flow.ClientID, &flow.RedirectURI, &flow.State,
		&flow.CodeChallenge, &flow.CodeChallengeMethod, &flow.AuthCode,
		&flow.SessionID, &flow.Status, &flow.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("flow not found")
		}
		return nil, fmt.Errorf("get pending flow: %w", err)
	}
	return &flow, nil
}

// ConsumePendingFlow atomically marks a connected flow as consumed and returns it.
// It looks up the flow by auth_code (the "code" param in the token exchange).
func (s *Store) ConsumePendingFlow(ctx context.Context, authCode string) (*oauth.PendingFlow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var flow oauth.PendingFlow
	err = tx.QueryRow(ctx, `
		SELECT uuid, tenant_id::text, client_id, redirect_uri, state,
			   code_challenge, code_challenge_method, auth_code,
			   COALESCE(session_id::text, ''), status, expires_at
		FROM oauth_pending_flows
		WHERE auth_code = $1
		FOR UPDATE
	`, authCode).Scan(
		&flow.UUID, &flow.TenantID, &flow.ClientID, &flow.RedirectURI, &flow.State,
		&flow.CodeChallenge, &flow.CodeChallengeMethod, &flow.AuthCode,
		&flow.SessionID, &flow.Status, &flow.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("flow not found")
		}
		return nil, fmt.Errorf("lookup flow by code: %w", err)
	}

	if flow.Status != "connected" {
		return nil, fmt.Errorf("flow not connected (status: %s)", flow.Status)
	}

	if flow.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("flow expired")
	}

	_, err = tx.Exec(ctx, `
		UPDATE oauth_pending_flows SET status = 'consumed' WHERE uuid = $1
	`, flow.UUID)
	if err != nil {
		return nil, fmt.Errorf("consume flow: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	flow.Status = "consumed"
	return &flow, nil
}

// GetTenantOAuth retrieves OAuth-specific tenant configuration.
// The tenantID parameter here is the client_id, which maps to the tenant ID.
func (s *Store) GetTenantOAuth(ctx context.Context, tenantID string) (*oauth.TenantOAuthConfig, error) {
	var cfg oauth.TenantOAuthConfig
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, oauth_redirect_uris, merchant_login_url
		FROM tenants WHERE id = $1
	`, tenantID).Scan(&cfg.ID, &cfg.RedirectURIs, &cfg.MerchantLoginURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("tenant not found")
		}
		return nil, fmt.Errorf("get tenant oauth config: %w", err)
	}
	return &cfg, nil
}

// OAuthSessionRecord holds session fields needed by OAuth token exchange.
type OAuthSessionRecord = oauth.SessionRecord

// GetOAuthSession retrieves a session by ID for OAuth token exchange.
func (s *Store) GetOAuthSession(ctx context.Context, id string) (*oauth.SessionRecord, error) {
	var rec oauth.SessionRecord
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, status, scopes, refresh_count, expires_at
		FROM sessions WHERE id = $1
	`, id).Scan(&rec.ID, &rec.TenantID, &status, &rec.Scopes, &rec.RefreshCount, &rec.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("session not found")
		}
		return nil, fmt.Errorf("get oauth session: %w", err)
	}
	rec.Status = session.Status(status)
	return &rec, nil
}

// GetTenantPolicy retrieves a tenant's session policy constraints.
func (s *Store) GetTenantPolicy(ctx context.Context, tenantID string) (*oauth.TenantPolicy, error) {
	var policy oauth.TenantPolicy
	err := s.pool.QueryRow(ctx, `
		SELECT session_max_expiry, max_refreshes
		FROM tenants WHERE id = $1
	`, tenantID).Scan(&policy.SessionMaxExpiry, &policy.MaxRefreshes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("tenant not found")
		}
		return nil, fmt.Errorf("get tenant policy: %w", err)
	}
	return &policy, nil
}

// OAuthFlowStore wraps Store to satisfy oauth.FlowStore.
type OAuthFlowStore struct{ *Store }

// OAuthSessionStore wraps Store to satisfy oauth.SessionStore.
type OAuthSessionStore struct{ *Store }

// GetSession retrieves a session for OAuth token exchange.
func (o *OAuthSessionStore) GetSession(ctx context.Context, id string) (*oauth.SessionRecord, error) {
	return o.GetOAuthSession(ctx, id)
}

// Ensure compile-time interface satisfaction.
var (
	_ oauth.FlowStore    = (*OAuthFlowStore)(nil)
	_ oauth.SessionStore = (*OAuthSessionStore)(nil)
)
