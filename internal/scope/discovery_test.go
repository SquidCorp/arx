package scope_test

import (
	"testing"

	"github.com/fambr/arx/internal/scope"
)

func TestFilterTools(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "add-to-cart", CatalogType: "cart.add", RequiredScopes: []string{"cart:write"}},
		{Name: "view-cart", CatalogType: "cart.view", RequiredScopes: []string{"cart:read"}},
		{Name: "checkout", CatalogType: "checkout.exec", RequiredScopes: []string{"checkout:exec"}},
		{Name: "search", CatalogType: "products.search", RequiredScopes: []string{"products:read"}},
	}

	tests := []struct {
		name          string
		sessionScopes []string
		wantNames     []string
	}{
		{
			name:          "all scopes returns all tools",
			sessionScopes: []string{"cart:write", "cart:read", "checkout:exec", "products:read"},
			wantNames:     []string{"add-to-cart", "view-cart", "checkout", "search"},
		},
		{
			name:          "limited scopes filters tools",
			sessionScopes: []string{"cart:read"},
			wantNames:     []string{"view-cart"},
		},
		{
			name:          "no scopes returns empty",
			sessionScopes: []string{},
			wantNames:     []string{},
		},
		{
			name:          "scopes with constraints still match",
			sessionScopes: []string{"checkout:exec:maxAmount=100", "cart:read"},
			wantNames:     []string{"view-cart", "checkout"},
		},
		{
			name:          "unrelated scopes returns empty",
			sessionScopes: []string{"admin:write"},
			wantNames:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filtered := scope.FilterTools(tools, tt.sessionScopes)

			if len(filtered) != len(tt.wantNames) {
				t.Fatalf("got %d tools, want %d", len(filtered), len(tt.wantNames))
			}

			got := make(map[string]bool, len(filtered))
			for _, tool := range filtered {
				got[tool.Name] = true
			}
			for _, name := range tt.wantNames {
				if !got[name] {
					t.Errorf("missing expected tool %q", name)
				}
			}
		})
	}
}

func TestFilterToolsPreservesOrder(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "a", RequiredScopes: []string{"x:read"}},
		{Name: "b", RequiredScopes: []string{"x:read"}},
		{Name: "c", RequiredScopes: []string{"x:read"}},
	}

	filtered := scope.FilterTools(tools, []string{"x:read"})
	if len(filtered) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(filtered))
	}
	for i, want := range []string{"a", "b", "c"} {
		if filtered[i].Name != want {
			t.Errorf("filtered[%d].Name = %q, want %q", i, filtered[i].Name, want)
		}
	}
}

func TestFilterToolsWithNoRequiredScopes(t *testing.T) {
	t.Parallel()

	tools := []scope.Tool{
		{Name: "public-tool", RequiredScopes: []string{}},
		{Name: "gated-tool", RequiredScopes: []string{"admin:write"}},
	}

	filtered := scope.FilterTools(tools, []string{})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(filtered))
	}
	if filtered[0].Name != "public-tool" {
		t.Errorf("expected public-tool, got %q", filtered[0].Name)
	}
}
