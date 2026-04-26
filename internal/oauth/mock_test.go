package oauth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fambr/arx/internal/session"
)

// mockFlowStore is an in-memory FlowStore for testing.
type mockFlowStore struct {
	mu    sync.Mutex
	flows map[string]*PendingFlow

	tenants map[string]*TenantOAuthConfig
}

func newMockFlowStore() *mockFlowStore {
	return &mockFlowStore{
		flows:   make(map[string]*PendingFlow),
		tenants: make(map[string]*TenantOAuthConfig),
	}
}

func (m *mockFlowStore) CreatePendingFlow(_ context.Context, flow *PendingFlow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flows[flow.UUID] = flow
	return nil
}

func (m *mockFlowStore) GetPendingFlow(_ context.Context, uuid string) (*PendingFlow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flows[uuid]
	if !ok {
		return nil, errors.New("flow not found")
	}
	return f, nil
}

func (m *mockFlowStore) ConsumePendingFlow(_ context.Context, uuid string) (*PendingFlow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flows[uuid]
	if !ok {
		return nil, errors.New("flow not found")
	}
	if f.Status == "consumed" {
		return nil, errors.New("flow already consumed")
	}
	if f.Status != "connected" {
		return nil, errors.New("flow not connected")
	}
	if f.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("flow expired")
	}
	f.Status = "consumed"
	return f, nil
}

func (m *mockFlowStore) GetTenantOAuth(_ context.Context, tenantID string) (*TenantOAuthConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, errors.New("tenant not found")
	}
	return t, nil
}

// mockSessionStore is an in-memory SessionStore for testing.
type mockSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*SessionRecord
	policy   *TenantPolicy
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{
		sessions: make(map[string]*SessionRecord),
		policy: &TenantPolicy{
			SessionMaxExpiry: 1 * time.Hour,
			MaxRefreshes:     5,
		},
	}
}

func (m *mockSessionStore) GetSession(_ context.Context, id string) (*SessionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return s, nil
}

func (m *mockSessionStore) RefreshSession(_ context.Context, id string, expiresAt time.Time, scopes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return errors.New("session not found")
	}
	s.ExpiresAt = expiresAt
	s.Scopes = scopes
	s.RefreshCount++
	return nil
}

func (m *mockSessionStore) GetTenantPolicy(_ context.Context, _ string) (*TenantPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.policy, nil
}

// testSession creates a default active session for testing.
func testSession(id, tenantID string) *SessionRecord {
	return &SessionRecord{
		ID:           id,
		TenantID:     tenantID,
		Status:       session.StatusActive,
		Scopes:       []string{"cart.view", "cart.add"},
		RefreshCount: 0,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
}
