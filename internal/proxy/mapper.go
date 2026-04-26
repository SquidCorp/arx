// Package proxy implements the reverse proxy engine that transforms standard
// catalog parameters to merchant-specific field paths and forwards requests
// to upstream endpoints.
package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Location represents where a parameter should be placed in the upstream request.
type Location string

const (
	// LocationBody places the parameter in the JSON request body.
	LocationBody Location = "body"
	// LocationQuery places the parameter in the URL query string.
	LocationQuery Location = "query"
)

// FieldMapping describes where a single parameter should be placed in the
// upstream request.
type FieldMapping struct {
	// Location is the target location (body or query).
	Location Location
	// Field is the target field name at that location.
	Field string
}

// Mapping is a map from standard catalog parameter names to their upstream
// field mappings.
type Mapping map[string]FieldMapping

// MappedRequest holds the transformed parameters split by location.
type MappedRequest struct {
	// Body contains parameters destined for the JSON request body.
	Body map[string]any
	// Query contains parameters destined for the URL query string.
	Query map[string]any
}

// ParseMapping parses a JSON param_mapping string into a structured Mapping.
// Each value must be in the format "location.field" where location is one of
// "body" or "query". Returns an error if the JSON is invalid or any path is
// malformed.
func ParseMapping(raw string) (Mapping, error) {
	var rawMap map[string]string
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		return nil, fmt.Errorf("invalid param_mapping JSON: %w", err)
	}

	m := make(Mapping, len(rawMap))
	for param, path := range rawMap {
		parts := strings.SplitN(path, ".", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid mapping path %q: must be location.field", path)
		}

		loc := Location(parts[0])
		field := parts[1]

		if loc != LocationBody && loc != LocationQuery {
			return nil, fmt.Errorf("invalid mapping path %q: unknown location %q (must be body or query)", path, loc)
		}
		if field == "" {
			return nil, fmt.Errorf("invalid mapping path %q: empty field name", path)
		}

		m[param] = FieldMapping{Location: loc, Field: field}
	}

	return m, nil
}

// MapParams applies a Mapping to the given tool call parameters. Parameters
// present in the mapping are placed at their configured location and field.
// Parameters not in the mapping are dropped. Parameters in the mapping but
// absent from params are omitted from the result.
func MapParams(m Mapping, params map[string]any) MappedRequest {
	result := MappedRequest{
		Body:  make(map[string]any),
		Query: make(map[string]any),
	}

	for paramName, fm := range m {
		val, exists := params[paramName]
		if !exists {
			continue
		}

		switch fm.Location {
		case LocationBody:
			result.Body[fm.Field] = val
		case LocationQuery:
			result.Query[fm.Field] = val
		}
	}

	return result
}
