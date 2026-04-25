// Package handler provides HTTP request handlers.
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/store"
	"github.com/fambr/arx/internal/token"
	"github.com/fambr/arx/internal/webhook"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	db    *pgxpool.Pool
	river *river.Client[pgx.Tx]
}

// Register mounts all HTTP routes on the given router.
func Register(
	r chi.Router,
	db *pgxpool.Pool,
	riverClient *river.Client[pgx.Tx],
	cacheClient *cache.Client,
	keyManager crypto.KeyManager,
	issuer *token.Issuer,
) {
	h := &Handler{db: db, river: riverClient}

	// Caches.
	sessionCache := cache.NewSessionCache(cacheClient)
	nonceStore := cache.NewNonceStore(cacheClient)

	// Database store.
	dbStore := store.NewStore(db)

	// Tenant key lookup.
	keyLookup := func(ctx context.Context, tenantID string) (crypto.TenantKeys, error) {
		return dbStore.TenantKeys(ctx, tenantID)
	}

	// Webhook handlers and middleware.
	webhookHandler := webhook.NewHandler(dbStore, sessionCache, dbStore)
	sigValidator := webhook.NewSignatureValidator(keyLookup, keyManager, nonceStore)

	// Token handlers and middleware.
	bindHandler := token.NewBindHandler(dbStore, sessionCache, keyLookup, issuer, nonceStore)
	tokenValidator := token.NewValidator(
		keyLookup,
		&store.TokenSessionReader{Store: dbStore},
		issuer.Config().Issuer,
		issuer.Config().Audience,
		func(jti string) (bool, error) {
			return nonceStore.CheckDPoPNonce(context.Background(), jti)
		},
	)

	// Health check.
	r.Get("/health", h.Health)

	// Webhook routes (signature-validated).
	r.With(sigValidator.Middleware).Post("/webhook/session-connected", webhookHandler.SessionConnected)
	r.With(sigValidator.Middleware).Post("/webhook/session-refresh", webhookHandler.SessionRefresh)
	r.With(sigValidator.Middleware).Post("/webhook/session-revoked", webhookHandler.SessionRevoked)
	r.With(sigValidator.Middleware).Post("/webhook/session-resumed", webhookHandler.SessionResumed)

	// Token bind route.
	r.Post("/token/bind", bindHandler.Bind)

	// Protected resource route (token-validated).
	r.Get("/resource", tokenValidator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})).ServeHTTP)
}

// Health returns the service health status.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
