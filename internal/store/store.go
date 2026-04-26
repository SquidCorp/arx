// Package store provides database-backed implementations of the storage
// interfaces used by the webhook and token packages.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/session"
	"github.com/fambr/arx/internal/token"
	"github.com/fambr/arx/internal/webhook"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store provides database operations for sessions, tenants, and OAuth flows.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateSession inserts a new session and returns its generated ID.
func (s *Store) CreateSession(ctx context.Context, rec *webhook.SessionRecord) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, merchant_sid, user_id, is_anonymous,
			status, scopes, claims_enc, bind_code, bind_code_expires_at,
			expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id::text
	`, rec.TenantID, rec.MerchantSID, rec.UserID, rec.IsAnonymous,
		string(rec.Status), rec.Scopes, rec.ClaimsEnc, rec.BindCode, rec.BindCodeExpires,
		rec.ExpiresAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return id, nil
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(ctx context.Context, id string) (*webhook.SessionRecord, error) {
	var rec webhook.SessionRecord
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT
			id::text, tenant_id::text, merchant_sid, user_id, is_anonymous,
			status, scopes, claims_enc, bind_code, bind_code_expires_at,
			refresh_count, expires_at, created_at, updated_at
		FROM sessions WHERE id = $1
	`, id).Scan(
		&rec.ID, &rec.TenantID, &rec.MerchantSID, &rec.UserID, &rec.IsAnonymous,
		&status, &rec.Scopes, &rec.ClaimsEnc, &rec.BindCode, &rec.BindCodeExpires,
		&rec.RefreshCount, &rec.ExpiresAt, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, webhook.ErrSessionNotFound
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	rec.Status = session.Status(status)
	return &rec, nil
}

// UpdateSessionStatus updates a session's status.
func (s *Store) UpdateSessionStatus(ctx context.Context, id string, status session.Status) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET status = $1, updated_at = NOW() WHERE id = $2
	`, string(status), id)
	if err != nil {
		return fmt.Errorf("update session status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return webhook.ErrSessionNotFound
	}
	return nil
}

// RefreshSession updates expiry, scopes, and increments refresh_count.
func (s *Store) RefreshSession(ctx context.Context, id string, expiresAt time.Time, scopes []string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions
		SET expires_at = $1, scopes = $2, refresh_count = refresh_count + 1, updated_at = NOW()
		WHERE id = $3
	`, expiresAt, scopes, id)
	if err != nil {
		return fmt.Errorf("refresh session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return webhook.ErrSessionNotFound
	}
	return nil
}

// SetRefreshCount sets a session's refresh_count to the given value.
func (s *Store) SetRefreshCount(ctx context.Context, id string, count int) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET refresh_count = $1, updated_at = NOW() WHERE id = $2
	`, count, id)
	if err != nil {
		return fmt.Errorf("set refresh count: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return webhook.ErrSessionNotFound
	}
	return nil
}

// GetTenant retrieves a tenant's session policy.
func (s *Store) GetTenant(ctx context.Context, tenantID string) (*webhook.TenantRecord, error) {
	var rec webhook.TenantRecord
	var maxExpiry time.Duration
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, session_max_expiry, max_refreshes
		FROM tenants WHERE id = $1
	`, tenantID).Scan(&rec.ID, &maxExpiry, &rec.MaxRefreshes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, webhook.ErrSessionNotFound
		}
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	rec.SessionMaxExpiry = maxExpiry
	return &rec, nil
}

// GetAndConsumeBindCode atomically looks up a session by bind code,
// validates it has not expired, and consumes it.
func (s *Store) GetAndConsumeBindCode(ctx context.Context, bindCode string) (*token.BindSession, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var sess token.BindSession
	var status string
	var bindCodeValue *string
	var bindCodeExpires *time.Time
	err = tx.QueryRow(ctx, `
		SELECT
			id::text, tenant_id::text, status, scopes, bind_code, bind_code_expires_at, expires_at
		FROM sessions WHERE bind_code = $1
		FOR UPDATE
	`, bindCode).Scan(
		&sess.ID, &sess.TenantID, &status, &sess.Scopes,
		&bindCodeValue, &bindCodeExpires, &sess.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, token.ErrBindCodeNotFound
		}
		return nil, fmt.Errorf("lookup bind code: %w", err)
	}

	if bindCodeValue == nil {
		return nil, token.ErrBindCodeAlreadyUsed
	}
	if bindCodeExpires != nil && bindCodeExpires.Before(time.Now()) {
		return nil, token.ErrBindCodeExpired
	}

	sess.Status = session.Status(status)

	_, err = tx.Exec(ctx, `
		UPDATE sessions
		SET bind_code = NULL, bind_code_expires_at = NULL, updated_at = NOW()
		WHERE id = $1
	`, sess.ID)
	if err != nil {
		return nil, fmt.Errorf("consume bind code: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &sess, nil
}

// LinkSessionToFlow links a session to a pending OAuth flow.
func (s *Store) LinkSessionToFlow(ctx context.Context, uuid, sessionID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE oauth_pending_flows
		SET session_id = $2, status = 'connected', updated_at = NOW()
		WHERE uuid = $1 AND status = 'pending' AND expires_at > NOW()
	`, uuid, sessionID)
	if err != nil {
		return fmt.Errorf("link session to flow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("flow not found or expired")
	}
	return nil
}

// GetSessionInfo retrieves session info for the token middleware.
func (s *Store) GetSessionInfo(ctx context.Context, id string) (*token.SessionInfo, error) {
	var sess token.SessionInfo
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, user_id, status, scopes, expires_at
		FROM sessions WHERE id = $1
	`, id).Scan(&sess.ID, &sess.TenantID, &sess.UserID, &status, &sess.Scopes, &sess.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("session not found")
		}
		return nil, fmt.Errorf("get session info: %w", err)
	}
	sess.Status = session.Status(status)
	return &sess, nil
}

// TenantKeys resolves a tenant's cryptographic keys by tenant ID.
func (s *Store) TenantKeys(ctx context.Context, tenantID string) (crypto.TenantKeys, error) {
	var keys crypto.TenantKeys
	err := s.pool.QueryRow(ctx, `
		SELECT
			merchant_public_key,
			merchant_public_key_previous,
			merchant_key_grace_until,
			arx_signing_public_key,
			arx_signing_private_key_enc,
			dek_enc
		FROM tenants WHERE id = $1
	`, tenantID).Scan(
		&keys.MerchantPublicKey,
		&keys.MerchantPublicKeyPrevious,
		&keys.MerchantKeyGraceUntil,
		&keys.ArxSigningPublicKey,
		&keys.ArxSigningPrivateKeyEnc,
		&keys.DEKEnc,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return keys, errors.New("tenant not found")
		}
		return keys, fmt.Errorf("lookup tenant keys: %w", err)
	}
	return keys, nil
}

// TokenSessionReader adapts Store to token.SessionReader.
type TokenSessionReader struct{ *Store }

// GetSession implements token.SessionReader.
func (t *TokenSessionReader) GetSession(ctx context.Context, id string) (*token.SessionInfo, error) {
	return t.GetSessionInfo(ctx, id)
}

// Ensure compile-time interface satisfaction.
var (
	_ webhook.SessionStore    = (*Store)(nil)
	_ webhook.OAuthFlowLinker = (*Store)(nil)
	_ token.BindSessionStore  = (*Store)(nil)
	_ token.SessionReader     = (*TokenSessionReader)(nil)
)
