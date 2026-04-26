// Package scope implements the scope enforcement engine for Arx. It provides
// a standard tool catalog with typed parameter schemas, scope matching,
// structured constraint parsing and evaluation, and scope-filtered tool
// discovery.
package scope

import (
	"errors"
	"sort"
)

// ParamType represents the data type of a tool parameter.
type ParamType string

const (
	// ParamTypeString is a string parameter.
	ParamTypeString ParamType = "string"
	// ParamTypeNumber is a numeric parameter (integer or float).
	ParamTypeNumber ParamType = "number"
	// ParamTypeBoolean is a boolean parameter.
	ParamTypeBoolean ParamType = "boolean"
)

// ParamSchema describes a single parameter in a tool's input schema.
type ParamSchema struct {
	// Name is the parameter identifier used in tool calls.
	Name string `json:"name"`
	// Type is the expected data type.
	Type ParamType `json:"type"`
	// Description explains the parameter's purpose.
	Description string `json:"description"`
	// Required indicates whether the parameter must be provided.
	Required bool `json:"required"`
}

// CatalogEntry defines a standard tool type with its parameter schema and
// required scopes.
type CatalogEntry struct {
	// Type is the unique catalog identifier (e.g., "cart.add").
	Type string `json:"type"`
	// Description explains what the tool does.
	Description string `json:"description"`
	// RequiredScopes lists scopes needed to call this tool.
	RequiredScopes []string `json:"required_scopes"`
	// Params defines the tool's typed parameter schema.
	Params []ParamSchema `json:"params"`
}

// ErrUnknownCatalogType is returned when a catalog type is not found.
var ErrUnknownCatalogType = errors.New("unknown_catalog_type")

// catalog holds the built-in standard tool types.
var catalog map[string]CatalogEntry

func init() {
	catalog = make(map[string]CatalogEntry, 6)
	for _, e := range builtinEntries() {
		catalog[e.Type] = e
	}
}

// GetCatalogEntry returns the catalog entry for the given type.
// The second return value is false if the type is not found.
func GetCatalogEntry(catalogType string) (CatalogEntry, bool) {
	e, ok := catalog[catalogType]
	return e, ok
}

// CatalogTypes returns a sorted list of all registered catalog type names.
func CatalogTypes() []string {
	types := make([]string, 0, len(catalog))
	for t := range catalog {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// ValidateCatalogType returns ErrUnknownCatalogType if the given type is not
// in the catalog.
func ValidateCatalogType(catalogType string) error {
	if _, ok := catalog[catalogType]; !ok {
		return ErrUnknownCatalogType
	}
	return nil
}
