package scope_test

import (
	"testing"

	"github.com/fambr/arx/internal/scope"
)

func TestMatchScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		sessionScopes  []string
		requiredScopes []string
		want           bool
	}{
		{
			name:           "superset allows",
			sessionScopes:  []string{"cart:read", "cart:write", "checkout:exec"},
			requiredScopes: []string{"cart:write"},
			want:           true,
		},
		{
			name:           "exact match allows",
			sessionScopes:  []string{"cart:read"},
			requiredScopes: []string{"cart:read"},
			want:           true,
		},
		{
			name:           "multiple required all present",
			sessionScopes:  []string{"cart:read", "cart:write", "checkout:exec"},
			requiredScopes: []string{"cart:read", "cart:write"},
			want:           true,
		},
		{
			name:           "missing scope denies",
			sessionScopes:  []string{"cart:read"},
			requiredScopes: []string{"cart:write"},
			want:           false,
		},
		{
			name:           "partial overlap denies",
			sessionScopes:  []string{"cart:read", "checkout:exec"},
			requiredScopes: []string{"cart:read", "cart:write"},
			want:           false,
		},
		{
			name:           "empty required allows",
			sessionScopes:  []string{"cart:read"},
			requiredScopes: []string{},
			want:           true,
		},
		{
			name:           "nil required allows",
			sessionScopes:  []string{"cart:read"},
			requiredScopes: nil,
			want:           true,
		},
		{
			name:           "empty session denies non-empty required",
			sessionScopes:  []string{},
			requiredScopes: []string{"cart:read"},
			want:           false,
		},
		{
			name:           "scopes with constraints match base scope",
			sessionScopes:  []string{"checkout:exec:maxAmount=100"},
			requiredScopes: []string{"checkout:exec"},
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := scope.MatchScopes(tt.sessionScopes, tt.requiredScopes)
			if got != tt.want {
				t.Errorf("MatchScopes(%v, %v) = %v, want %v",
					tt.sessionScopes, tt.requiredScopes, got, tt.want)
			}
		})
	}
}

func TestMissingScopes(t *testing.T) {
	t.Parallel()

	t.Run("returns missing scopes", func(t *testing.T) {
		t.Parallel()
		missing := scope.MissingScopes(
			[]string{"cart:read"},
			[]string{"cart:read", "cart:write", "checkout:exec"},
		)
		if len(missing) != 2 {
			t.Fatalf("expected 2 missing scopes, got %d: %v", len(missing), missing)
		}
		want := map[string]bool{"cart:write": true, "checkout:exec": true}
		for _, s := range missing {
			if !want[s] {
				t.Errorf("unexpected missing scope %q", s)
			}
		}
	})

	t.Run("returns nil when all present", func(t *testing.T) {
		t.Parallel()
		missing := scope.MissingScopes(
			[]string{"cart:read", "cart:write"},
			[]string{"cart:read"},
		)
		if len(missing) != 0 {
			t.Errorf("expected no missing scopes, got %v", missing)
		}
	})
}
