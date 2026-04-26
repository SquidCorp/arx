package scope

// Tool represents a tenant-registered tool with its required scopes. This is
// the minimal view used for scope-filtered discovery; full tool details live in
// the store/cache layers.
type Tool struct {
	// Name is the tool's tenant-unique identifier.
	Name string
	// CatalogType is the standard catalog type (e.g., "cart.add").
	CatalogType string
	// RequiredScopes lists scopes needed to call this tool.
	RequiredScopes []string
}

// FilterTools returns the subset of tools whose RequiredScopes are fully
// satisfied by sessionScopes. The result preserves the input order.
func FilterTools(tools []Tool, sessionScopes []string) []Tool {
	// Pre-compute the set of base scopes the session holds.
	have := make(map[string]bool, len(sessionScopes))
	for _, s := range sessionScopes {
		have[baseScope(s)] = true
	}

	filtered := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if allScopesCovered(have, tool.RequiredScopes) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// allScopesCovered checks whether every required scope is present in the set.
func allScopesCovered(have map[string]bool, required []string) bool {
	for _, req := range required {
		if !have[req] {
			return false
		}
	}
	return true
}
