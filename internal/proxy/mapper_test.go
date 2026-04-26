package proxy

import (
	"testing"
)

func TestParseMapping_ValidBodyFields(t *testing.T) {
	raw := `{"product_id":"body.item_id","quantity":"body.qty"}`
	m, err := ParseMapping(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("got %d entries, want 2", len(m))
	}
	if m["product_id"].Location != LocationBody || m["product_id"].Field != "item_id" {
		t.Errorf("product_id mapping = %+v, want body.item_id", m["product_id"])
	}
	if m["quantity"].Location != LocationBody || m["quantity"].Field != "qty" {
		t.Errorf("quantity mapping = %+v, want body.qty", m["quantity"])
	}
}

func TestParseMapping_QueryFields(t *testing.T) {
	raw := `{"query":"query.q","limit":"query.max_results"}`
	m, err := ParseMapping(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["query"].Location != LocationQuery || m["query"].Field != "q" {
		t.Errorf("query mapping = %+v, want query.q", m["query"])
	}
	if m["limit"].Location != LocationQuery || m["limit"].Field != "max_results" {
		t.Errorf("limit mapping = %+v, want query.max_results", m["limit"])
	}
}

func TestParseMapping_MixedLocations(t *testing.T) {
	raw := `{"product_id":"body.pid","category":"query.cat"}`
	m, err := ParseMapping(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["product_id"].Location != LocationBody {
		t.Error("product_id should map to body")
	}
	if m["category"].Location != LocationQuery {
		t.Error("category should map to query")
	}
}

func TestParseMapping_EmptyJSON(t *testing.T) {
	m, err := ParseMapping(`{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("got %d entries, want 0", len(m))
	}
}

func TestParseMapping_InvalidJSON(t *testing.T) {
	_, err := ParseMapping(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseMapping_InvalidPath_NoPrefix(t *testing.T) {
	_, err := ParseMapping(`{"foo":"invalid_path"}`)
	if err == nil {
		t.Fatal("expected error for path without location prefix")
	}
}

func TestParseMapping_InvalidPath_UnknownLocation(t *testing.T) {
	_, err := ParseMapping(`{"foo":"header.x"}`)
	if err == nil {
		t.Fatal("expected error for unknown location prefix")
	}
}

func TestParseMapping_InvalidPath_EmptyField(t *testing.T) {
	_, err := ParseMapping(`{"foo":"body."}`)
	if err == nil {
		t.Fatal("expected error for empty field name")
	}
}

func TestMapParams_BasicBody(t *testing.T) {
	m, _ := ParseMapping(`{"product_id":"body.item_id","quantity":"body.qty"}`)
	params := map[string]any{
		"product_id": "SKU-42",
		"quantity":   3,
	}

	result := MapParams(m, params)

	if result.Body["item_id"] != "SKU-42" {
		t.Errorf("body.item_id = %v, want SKU-42", result.Body["item_id"])
	}
	if result.Body["qty"] != 3 {
		t.Errorf("body.qty = %v, want 3", result.Body["qty"])
	}
	if len(result.Query) != 0 {
		t.Errorf("query should be empty, got %v", result.Query)
	}
}

func TestMapParams_BasicQuery(t *testing.T) {
	m, _ := ParseMapping(`{"query":"query.q","limit":"query.max"}`)
	params := map[string]any{
		"query": "shoes",
		"limit": 10,
	}

	result := MapParams(m, params)

	if result.Query["q"] != "shoes" {
		t.Errorf("query.q = %v, want shoes", result.Query["q"])
	}
	if result.Query["max"] != 10 {
		t.Errorf("query.max = %v, want 10", result.Query["max"])
	}
	if len(result.Body) != 0 {
		t.Errorf("body should be empty, got %v", result.Body)
	}
}

func TestMapParams_UnmappedParamsDropped(t *testing.T) {
	m, _ := ParseMapping(`{"product_id":"body.pid"}`)
	params := map[string]any{
		"product_id": "SKU-42",
		"unknown":    "should be dropped",
		"extra":      999,
	}

	result := MapParams(m, params)

	if len(result.Body) != 1 {
		t.Errorf("body should have 1 entry, got %d", len(result.Body))
	}
	if result.Body["pid"] != "SKU-42" {
		t.Errorf("body.pid = %v, want SKU-42", result.Body["pid"])
	}
}

func TestMapParams_MissingMappedParam(t *testing.T) {
	m, _ := ParseMapping(`{"product_id":"body.pid","quantity":"body.qty"}`)
	params := map[string]any{
		"product_id": "SKU-42",
		// quantity not provided
	}

	result := MapParams(m, params)

	if len(result.Body) != 1 {
		t.Errorf("body should have 1 entry, got %d", len(result.Body))
	}
	if _, exists := result.Body["qty"]; exists {
		t.Error("qty should not be present when quantity is not provided")
	}
}

func TestMapParams_EmptyMapping(t *testing.T) {
	m, _ := ParseMapping(`{}`)
	params := map[string]any{"foo": "bar"}

	result := MapParams(m, params)

	if len(result.Body) != 0 {
		t.Errorf("body should be empty, got %v", result.Body)
	}
	if len(result.Query) != 0 {
		t.Errorf("query should be empty, got %v", result.Query)
	}
}

func TestMapParams_NilParams(t *testing.T) {
	m, _ := ParseMapping(`{"product_id":"body.pid"}`)

	result := MapParams(m, nil)

	if len(result.Body) != 0 {
		t.Errorf("body should be empty, got %v", result.Body)
	}
	if len(result.Query) != 0 {
		t.Errorf("query should be empty, got %v", result.Query)
	}
}

func TestMapParams_MixedBodyAndQuery(t *testing.T) {
	m, _ := ParseMapping(`{"product_id":"body.pid","category":"query.cat","quantity":"body.qty"}`)
	params := map[string]any{
		"product_id": "SKU-42",
		"category":   "electronics",
		"quantity":   2,
	}

	result := MapParams(m, params)

	if result.Body["pid"] != "SKU-42" {
		t.Errorf("body.pid = %v, want SKU-42", result.Body["pid"])
	}
	if result.Body["qty"] != 2 {
		t.Errorf("body.qty = %v, want 2", result.Body["qty"])
	}
	if result.Query["cat"] != "electronics" {
		t.Errorf("query.cat = %v, want electronics", result.Query["cat"])
	}
}
