package scope_test

import (
	"testing"

	"github.com/fambr/arx/internal/scope"
)

func TestGetCatalogEntry(t *testing.T) {
	t.Parallel()

	t.Run("known type returns entry", func(t *testing.T) {
		t.Parallel()
		entry, ok := scope.GetCatalogEntry("cart.add")
		if !ok {
			t.Fatal("expected cart.add to exist in catalog")
		}
		if entry.Type != "cart.add" {
			t.Errorf("type = %q, want %q", entry.Type, "cart.add")
		}
		if entry.Description == "" {
			t.Error("expected non-empty description")
		}
		if len(entry.RequiredScopes) == 0 {
			t.Error("expected at least one required scope")
		}
		if len(entry.Params) == 0 {
			t.Error("expected at least one parameter")
		}
	})

	t.Run("unknown type returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := scope.GetCatalogEntry("nonexistent.tool")
		if ok {
			t.Error("expected false for unknown catalog type")
		}
	})
}

func TestCatalogTypes(t *testing.T) {
	t.Parallel()

	types := scope.CatalogTypes()
	if len(types) == 0 {
		t.Fatal("expected at least one catalog type")
	}

	expected := []string{
		"cart.add", "cart.remove", "cart.view",
		"checkout.exec", "orders.list", "products.search",
	}

	typeSet := make(map[string]bool, len(types))
	for _, ct := range types {
		typeSet[ct] = true
	}

	for _, e := range expected {
		if !typeSet[e] {
			t.Errorf("missing catalog type %q", e)
		}
	}
}

func TestParamSchemaTypes(t *testing.T) {
	t.Parallel()

	entry, ok := scope.GetCatalogEntry("cart.add")
	if !ok {
		t.Fatal("expected cart.add in catalog")
	}

	for _, p := range entry.Params {
		switch p.Type {
		case scope.ParamTypeString, scope.ParamTypeNumber, scope.ParamTypeBoolean:
			// valid
		default:
			t.Errorf("param %q has invalid type %q", p.Name, p.Type)
		}
	}
}

func TestValidateCatalogType(t *testing.T) {
	t.Parallel()

	t.Run("valid type", func(t *testing.T) {
		t.Parallel()
		if err := scope.ValidateCatalogType("cart.add"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		t.Parallel()
		err := scope.ValidateCatalogType("unknown.tool")
		if err == nil {
			t.Error("expected error for unknown catalog type")
		}
	})
}
