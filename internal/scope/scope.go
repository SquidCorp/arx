package scope

import "strings"

// baseScope extracts the resource:action portion from a scope string,
// stripping any trailing constraint segments (e.g. "checkout:exec:maxAmount=100"
// becomes "checkout:exec").
func baseScope(s string) string {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 {
		return s
	}
	// The second segment may contain "action:key=value"; split on first ":" and
	// check whether the remainder contains "=".
	action := parts[1]
	if idx := strings.Index(action, ":"); idx >= 0 {
		// There's a third colon-separated field inside the action segment —
		// but that can't happen with SplitN(3). Let's handle the 3-part case.
		return parts[0] + ":" + parts[1]
	}
	if len(parts) == 3 {
		// parts[2] is constraint segment(s); return resource:action only.
		return parts[0] + ":" + parts[1]
	}
	return s
}

// MatchScopes reports whether sessionScopes is a superset of requiredScopes.
// Scopes with constraints (e.g. "checkout:exec:maxAmount=100") match the base
// scope "checkout:exec".
func MatchScopes(sessionScopes, requiredScopes []string) bool {
	if len(requiredScopes) == 0 {
		return true
	}

	have := make(map[string]bool, len(sessionScopes))
	for _, s := range sessionScopes {
		have[baseScope(s)] = true
	}

	for _, req := range requiredScopes {
		if !have[req] {
			return false
		}
	}
	return true
}

// MissingScopes returns the elements of requiredScopes that are not covered by
// sessionScopes. Returns nil when all required scopes are present.
func MissingScopes(sessionScopes, requiredScopes []string) []string {
	have := make(map[string]bool, len(sessionScopes))
	for _, s := range sessionScopes {
		have[baseScope(s)] = true
	}

	var missing []string
	for _, req := range requiredScopes {
		if !have[req] {
			missing = append(missing, req)
		}
	}
	return missing
}
