package scope

// builtinEntries returns the standard tool catalog entries.
func builtinEntries() []CatalogEntry {
	return []CatalogEntry{
		{
			Type:           "cart.add",
			Description:    "Add an item to the shopping cart",
			RequiredScopes: []string{"cart:write"},
			Params: []ParamSchema{
				{Name: "product_id", Type: ParamTypeString, Description: "Product identifier", Required: true},
				{Name: "quantity", Type: ParamTypeNumber, Description: "Number of items to add", Required: true},
			},
		},
		{
			Type:           "cart.remove",
			Description:    "Remove an item from the shopping cart",
			RequiredScopes: []string{"cart:write"},
			Params: []ParamSchema{
				{Name: "product_id", Type: ParamTypeString, Description: "Product identifier", Required: true},
				{Name: "quantity", Type: ParamTypeNumber, Description: "Number of items to remove", Required: false},
			},
		},
		{
			Type:           "cart.view",
			Description:    "View the current shopping cart contents",
			RequiredScopes: []string{"cart:read"},
			Params:         []ParamSchema{},
		},
		{
			Type:           "checkout.exec",
			Description:    "Execute checkout for the current cart",
			RequiredScopes: []string{"checkout:exec"},
			Params: []ParamSchema{
				{Name: "amount", Type: ParamTypeNumber, Description: "Total amount to charge", Required: true},
				{Name: "currency", Type: ParamTypeString, Description: "ISO 4217 currency code", Required: true},
			},
		},
		{
			Type:           "orders.list",
			Description:    "List the user's orders",
			RequiredScopes: []string{"orders:read"},
			Params: []ParamSchema{
				{Name: "status", Type: ParamTypeString, Description: "Filter by order status", Required: false},
				{Name: "limit", Type: ParamTypeNumber, Description: "Maximum number of results", Required: false},
			},
		},
		{
			Type:           "products.search",
			Description:    "Search the product catalog",
			RequiredScopes: []string{"products:read"},
			Params: []ParamSchema{
				{Name: "query", Type: ParamTypeString, Description: "Search query string", Required: true},
				{Name: "category", Type: ParamTypeString, Description: "Filter by category", Required: false},
				{Name: "max_price", Type: ParamTypeNumber, Description: "Maximum price filter", Required: false},
			},
		},
	}
}
