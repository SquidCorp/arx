// Package handler provides HTTP request handlers.
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/fambr/arx/internal/cache"
	"github.com/fambr/arx/internal/crypto"
	"github.com/fambr/arx/internal/mcp"
	"github.com/fambr/arx/internal/oauth"
	"github.com/fambr/arx/internal/store"
	"github.com/fambr/arx/internal/testapi"
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

// Register mounts all HTTP routes on the given router. When testMode is true,
// additional internal endpoints are registered for e2e test fixture management.
func Register(
	r chi.Router,
	db *pgxpool.Pool,
	riverClient *river.Client[pgx.Tx],
	cacheClient *cache.Client,
	keyManager crypto.KeyManager,
	issuer *token.Issuer,
	testMode bool,
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

	// OAuth 2.1 handler.
	var oauthCache *cache.OAuthCache
	if cacheClient != nil {
		oauthCache = cache.NewOAuthCache(cacheClient)
	}
	oauthHandler := oauth.NewHandler(
		&store.OAuthFlowStore{Store: dbStore},
		&store.OAuthSessionStore{Store: dbStore},
		oauthCache,
		sessionCache,
		keyLookup,
		issuer,
		nonceStore,
	)

	// Health check.
	r.Get("/health", h.Health)

	// Webhook routes (signature-validated).
	r.With(sigValidator.Middleware).Post("/webhook/session-connected", webhookHandler.SessionConnected)
	r.With(sigValidator.Middleware).Post("/webhook/session-refresh", webhookHandler.SessionRefresh)
	r.With(sigValidator.Middleware).Post("/webhook/session-revoked", webhookHandler.SessionRevoked)
	r.With(sigValidator.Middleware).Post("/webhook/session-resumed", webhookHandler.SessionResumed)

	// OAuth 2.1 routes.
	r.Get("/.well-known/oauth-authorization-server", oauthHandler.Metadata)
	r.Get("/oauth/authorize", oauthHandler.Authorize)
	r.Post("/oauth/token", oauthHandler.Token)
	r.Get("/oauth/callback", oauthHandler.Callback)

	// Token bind route.
	r.Post("/token/bind", bindHandler.Bind)

	// MCP server route.
	mcpTokenExtractor := mcp.NewTokenExtractorAdapter(
		keyLookup,
		&store.TokenSessionReader{Store: dbStore},
		issuer.Config().Issuer,
		issuer.Config().Audience,
	)
	mcpToolProvider := mcp.NewDBToolProvider(db)
	mcpHandler := mcp.NewHandler(mcpTokenExtractor, mcpToolProvider, nil)
	r.Post("/mcp", mcpHandler.ServeHTTP)

	// Protected resource route (token-validated).
	r.Get("/resource", tokenValidator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})).ServeHTTP)

	// Test-only routes for e2e fixture management.
	if testMode {
		testHandler := testapi.NewHandler(dbStore)
		r.Post("/internal/test/sessions", testHandler.CreateFixtureSession)
		r.Patch("/internal/test/sessions/{id}", testHandler.UpdateSession)
	}
}

// Health returns the service health status.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
