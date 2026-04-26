package proxy

import (
	"context"
	"net/http"
	"strings"
)

// Header names for session context on outbound proxied requests.
const (
	// HeaderSession carries the session ID.
	HeaderSession = "X-Arx-Session"
	// HeaderUser carries the user ID or "anonymous".
	HeaderUser = "X-Arx-User"
	// HeaderScopes carries comma-separated scopes.
	HeaderScopes = "X-Arx-Scopes"
)

// SessionContext holds session metadata to inject as headers on proxied requests.
type SessionContext struct {
	// SessionID is the Arx session identifier.
	SessionID string
	// UserID is the authenticated user, or "anonymous" for unauthenticated sessions.
	UserID string
	// Scopes is the list of granted scopes.
	Scopes []string
}

type sessionCtxKey struct{}

// WithSessionContext stores a SessionContext in the given context.
func WithSessionContext(ctx context.Context, sc *SessionContext) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, sc)
}

// SessionContextFrom retrieves the SessionContext from the context, or nil if absent.
func SessionContextFrom(ctx context.Context) *SessionContext {
	sc, _ := ctx.Value(sessionCtxKey{}).(*SessionContext)
	return sc
}

// injectSessionHeaders sets X-Arx-Session, X-Arx-User, and X-Arx-Scopes headers
// on the outbound request if a SessionContext is present in the context.
func injectSessionHeaders(ctx context.Context, req *http.Request) {
	sc := SessionContextFrom(ctx)
	if sc == nil {
		return
	}

	req.Header.Set(HeaderSession, sc.SessionID)
	req.Header.Set(HeaderUser, sc.UserID)
	req.Header.Set(HeaderScopes, strings.Join(sc.Scopes, ","))
}
