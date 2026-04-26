package scope_test

import (
	"testing"

	"github.com/fambr/arx/internal/scope"
)

func TestParseConstraints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		scopeStr    string
		wantBase    string
		wantCount   int
		wantPairs   map[string]string
		wantIsValid bool
	}{
		{
			name:        "no constraints",
			scopeStr:    "cart:read",
			wantBase:    "cart:read",
			wantCount:   0,
			wantPairs:   nil,
			wantIsValid: true,
		},
		{
			name:        "single constraint",
			scopeStr:    "checkout:exec:maxAmount=100",
			wantBase:    "checkout:exec",
			wantCount:   1,
			wantPairs:   map[string]string{"maxAmount": "100"},
			wantIsValid: true,
		},
		{
			name:        "multiple constraints",
			scopeStr:    "checkout:exec:maxAmount=100:currency=EUR",
			wantBase:    "checkout:exec",
			wantCount:   2,
			wantPairs:   map[string]string{"maxAmount": "100", "currency": "EUR"},
			wantIsValid: true,
		},
		{
			name:        "invalid scope no colon",
			scopeStr:    "cartread",
			wantBase:    "cartread",
			wantCount:   0,
			wantIsValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parsed := scope.ParseScope(tt.scopeStr)
			if parsed.Base != tt.wantBase {
				t.Errorf("base = %q, want %q", parsed.Base, tt.wantBase)
			}
			if len(parsed.Constraints) != tt.wantCount {
				t.Fatalf("constraint count = %d, want %d", len(parsed.Constraints), tt.wantCount)
			}
			for _, c := range parsed.Constraints {
				want, ok := tt.wantPairs[c.Key]
				if !ok {
					t.Errorf("unexpected constraint key %q", c.Key)
					continue
				}
				if c.Value != want {
					t.Errorf("constraint %q = %q, want %q", c.Key, c.Value, want)
				}
			}
		})
	}
}

func TestFindConstraints(t *testing.T) {
	t.Parallel()

	scopes := []string{
		"cart:read",
		"checkout:exec:maxAmount=100:currency=EUR",
		"orders:read",
	}

	t.Run("finds constraints for matching base scope", func(t *testing.T) {
		t.Parallel()
		constraints := scope.FindConstraints(scopes, "checkout:exec")
		if len(constraints) != 2 {
			t.Fatalf("expected 2 constraints, got %d", len(constraints))
		}
	})

	t.Run("returns nil for scope without constraints", func(t *testing.T) {
		t.Parallel()
		constraints := scope.FindConstraints(scopes, "cart:read")
		if len(constraints) != 0 {
			t.Errorf("expected 0 constraints, got %d", len(constraints))
		}
	})

	t.Run("returns nil for missing scope", func(t *testing.T) {
		t.Parallel()
		constraints := scope.FindConstraints(scopes, "products:read")
		if len(constraints) != 0 {
			t.Errorf("expected 0 constraints, got %d", len(constraints))
		}
	})
}
