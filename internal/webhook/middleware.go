// Package webhook provides HTTP handlers and middleware for processing
// merchant webhook requests including signature validation, session lifecycle
// management, and state machine enforcement.
package webhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
)

const (
	// headerSignature is the header containing the base64-encoded Ed25519 signature.
	headerSignature = "X-Arx-Signature"
	// headerTimestamp is the header containing the Unix epoch timestamp.
	headerTimestamp = "X-Arx-Timestamp"
	// headerNonce is the header containing a unique nonce for replay protection.
	headerNonce = "X-Arx-Nonce"
	// headerTenantID is the header identifying the tenant.
	headerTenantID = "X-Arx-Tenant-ID"

	// timestampFreshness is the maximum age of a webhook timestamp (±5 minutes).
	timestampFreshness = 5 * time.Minute
)

// tenantKeyCtxKey is the context key for storing resolved TenantKeys.
type tenantKeyCtxKey struct{}

// TenantKeysFromContext extracts TenantKeys from the request context.
// Returns the keys and true if present, zero value and false otherwise.
func TenantKeysFromContext(ctx context.Context) (crypto.TenantKeys, bool) {
	keys, ok := ctx.Value(tenantKeyCtxKey{}).(crypto.TenantKeys)
	return keys, ok
}

// tenantIDCtxKey is the context key for storing the resolved tenant ID.
type tenantIDCtxKey struct{}

// TenantIDFromContext extracts the tenant ID from the request context.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(tenantIDCtxKey{}).(string)
	return id, ok
}

// bodyCtxKey is the context key for storing the buffered request body.
type bodyCtxKey struct{}

// BodyFromContext extracts the buffered request body from the context.
func BodyFromContext(ctx context.Context) ([]byte, bool) {
	body, ok := ctx.Value(bodyCtxKey{}).([]byte)
	return body, ok
}

// TenantKeyLookup resolves a tenant's cryptographic keys by tenant ID.
type TenantKeyLookup func(ctx context.Context, tenantID string) (crypto.TenantKeys, error)

// NowFunc returns the current time. Defaults to time.Now; override in tests.
type NowFunc func() time.Time

// SignatureValidator is HTTP middleware that validates webhook signatures.
// It verifies the Ed25519 signature, timestamp freshness, and nonce uniqueness
// on every inbound merchant webhook.
type SignatureValidator struct {
	keyLookup  TenantKeyLookup
	keyManager crypto.KeyManager
	nonces     *cache.NonceStore
	now        NowFunc
}

// NewSignatureValidator creates a SignatureValidator middleware.
func NewSignatureValidator(
	keyLookup TenantKeyLookup,
	keyManager crypto.KeyManager,
	nonces *cache.NonceStore,
) *SignatureValidator {
	return &SignatureValidator{
		keyLookup:  keyLookup,
		keyManager: keyManager,
		nonces:     nonces,
		now:        time.Now,
	}
}

// Middleware returns an http.Handler middleware that validates webhook signatures.
// On success it stores TenantKeys, tenant ID, and the buffered body in the context.
func (sv *SignatureValidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get(headerTenantID)
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "missing_tenant_id")
			return
		}

		sigB64 := r.Header.Get(headerSignature)
		if sigB64 == "" {
			writeError(w, http.StatusUnauthorized, "missing_signature")
			return
		}

		ts := r.Header.Get(headerTimestamp)
		if ts == "" {
			writeError(w, http.StatusUnauthorized, "missing_timestamp")
			return
		}

		nonce := r.Header.Get(headerNonce)
		if nonce == "" {
			writeError(w, http.StatusUnauthorized, "missing_nonce")
			return
		}

		// Check timestamp freshness.
		tsUnix, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_timestamp")
			return
		}

		now := sv.now()
		reqTime := time.Unix(tsUnix, 0)
		diff := now.Sub(reqTime)
		if math.Abs(diff.Seconds()) > timestampFreshness.Seconds() {
			writeError(w, http.StatusUnauthorized, "stale_timestamp")
			return
		}

		// Check nonce uniqueness.
		unique, err := sv.nonces.CheckWebhookNonce(r.Context(), tenantID, nonce)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		if !unique {
			writeError(w, http.StatusUnauthorized, "duplicate_nonce")
			return
		}

		// Read and buffer the body.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body")
			return
		}
		_ = r.Body.Close()

		// Decode signature.
		sig, err := base64.StdEncoding.DecodeString(sigB64)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_signature_encoding")
			return
		}

		// Look up tenant keys.
		keys, err := sv.keyLookup(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unknown_tenant")
			return
		}

		// Verify signature.
		payload := crypto.SignedPayload{
			Method:    r.Method,
			Path:      r.URL.Path,
			Body:      body,
			Timestamp: ts,
			Nonce:     nonce,
		}

		if err := sv.keyManager.VerifyWebhook(r.Context(), keys, payload, sig); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_signature")
			return
		}

		// Store resolved data in context for downstream handlers.
		ctx := r.Context()
		ctx = context.WithValue(ctx, tenantKeyCtxKey{}, keys)
		ctx = context.WithValue(ctx, tenantIDCtxKey{}, tenantID)
		ctx = context.WithValue(ctx, bodyCtxKey{}, body)

		// Replace request body so downstream handlers can read it too.
		r.Body = io.NopCloser(bytes.NewReader(body))

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
