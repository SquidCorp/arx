package mcp

// metaToolDefinitions returns the 4 static MCP meta-tools that Arx exposes.
// These tools provide scope-filtered access to the tenant tool catalog.
func metaToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_tools",
			Description: "List available tools for the current session, filtered by scopes",
			InputSchema: JSONSchema{
				Type:       "object",
				Properties: map[string]SchemaItem{},
			},
		},
		{
			Name:        "get_tool",
			Description: "Get the full schema for a specific tool by name",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]SchemaItem{
					"name": {Type: "string", Description: "The tool name to retrieve"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "call_tool",
			Description: "Execute a tool with the given parameters. Requires DPoP proof.",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]SchemaItem{
					"name":   {Type: "string", Description: "The tool name to call"},
					"params": {Type: "object", Description: "Tool-specific parameters"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "session_info",
			Description: "Get current session metadata including scopes, claims, expiry, and status",
			InputSchema: JSONSchema{
				Type:       "object",
				Properties: map[string]SchemaItem{},
			},
		},
	}
}
