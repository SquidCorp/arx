package scope

import "strings"

// Constraint represents a single key=value constraint extracted from a scope
// string such as "checkout:exec:maxAmount=100".
type Constraint struct {
	// Key is the constraint name (e.g., "maxAmount").
	Key string
	// Value is the constraint value as a string (e.g., "100").
	Value string
}

// ParsedScope is a scope string broken into its base (resource:action) and
// optional constraints.
type ParsedScope struct {
	// Base is the resource:action portion (e.g., "checkout:exec").
	Base string
	// Constraints holds key=value pairs parsed from the scope string.
	Constraints []Constraint
}

// ParseScope parses a scope string in the format
// "resource:action[:key=value[:key=value...]]" into a ParsedScope.
func ParseScope(s string) ParsedScope {
	parts := strings.SplitN(s, ":", 3)

	if len(parts) < 2 {
		return ParsedScope{Base: s}
	}

	base := parts[0] + ":" + parts[1]

	if len(parts) < 3 {
		return ParsedScope{Base: base}
	}

	// Parse constraint segments from the remainder.
	constraintStr := parts[2]
	segments := strings.Split(constraintStr, ":")

	constraints := make([]Constraint, 0, len(segments))
	for _, seg := range segments {
		eqIdx := strings.Index(seg, "=")
		if eqIdx < 0 {
			continue
		}
		constraints = append(constraints, Constraint{
			Key:   seg[:eqIdx],
			Value: seg[eqIdx+1:],
		})
	}

	return ParsedScope{
		Base:        base,
		Constraints: constraints,
	}
}

// FindConstraints returns all constraints from sessionScopes that apply to the
// given base scope (resource:action). Returns nil if no constraints are found.
func FindConstraints(sessionScopes []string, baseScope string) []Constraint {
	var constraints []Constraint
	for _, s := range sessionScopes {
		parsed := ParseScope(s)
		if parsed.Base == baseScope && len(parsed.Constraints) > 0 {
			constraints = append(constraints, parsed.Constraints...)
		}
	}
	return constraints
}
