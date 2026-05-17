// BL302 S3 — unit tests for ElicitationDispatcher.
package mcp

import (
	"context"
	"testing"
)

// TestElicitationDispatcher_Schemas verifies that the three built-in schemas
// are registered and accessible.
func TestElicitationDispatcher_Schemas(t *testing.T) {
	d := NewElicitationDispatcher(nil, nil)

	schemas := d.Schemas()
	expected := []string{"approval", "text_input", "choice"}
	for _, name := range expected {
		if _, ok := schemas[name]; !ok {
			t.Errorf("schema %q not registered; registered: %v", name, schemas)
		}
	}
}

// TestElicitationDispatcher_ApprovalSchemaShape verifies that the approval
// schema has the expected JSON Schema shape.
func TestElicitationDispatcher_ApprovalSchemaShape(t *testing.T) {
	d := NewElicitationDispatcher(nil, nil)
	schemas := d.Schemas()

	approval, ok := schemas["approval"]
	if !ok {
		t.Fatal("approval schema not found")
	}
	props, ok := approval["properties"].(map[string]any)
	if !ok {
		t.Fatal("approval schema missing 'properties'")
	}
	if _, ok := props["action"]; !ok {
		t.Error("approval schema missing 'action' property")
	}
}

// TestElicitationDispatcher_NoClientGraceful verifies that Elicit returns
// ErrElicitationNotSupported when no MCP client is connected.
func TestElicitationDispatcher_NoClientGraceful(t *testing.T) {
	d := NewElicitationDispatcher(nil, nil)
	_, err := d.Elicit(context.Background(), ElicitationRequest{
		Schema:  "approval",
		Message: "approve?",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != ErrElicitationNotSupported {
		t.Fatalf("expected ErrElicitationNotSupported, got %v", err)
	}
}

// TestElicitationDispatcher_UnknownSchema verifies that an unknown schema
// name returns a descriptive error.
func TestElicitationDispatcher_UnknownSchema(t *testing.T) {
	d := NewElicitationDispatcher(nil, nil)
	_, err := d.Elicit(context.Background(), ElicitationRequest{
		Schema:  "nonexistent_schema",
		Message: "test",
	})
	if err == nil {
		t.Fatal("expected error for unknown schema, got nil")
	}
}

// TestElicitationDispatcher_RegisterSchema verifies that custom schemas can be
// registered and are returned by Schemas().
func TestElicitationDispatcher_RegisterSchema(t *testing.T) {
	d := NewElicitationDispatcher(nil, nil)
	d.RegisterSchema("custom", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{"type": "string"},
		},
	})
	schemas := d.Schemas()
	if _, ok := schemas["custom"]; !ok {
		t.Error("custom schema not found after RegisterSchema")
	}
}

// TestBuildChoiceSchema verifies the choice schema enum is populated correctly.
func TestBuildChoiceSchema(t *testing.T) {
	opts := []string{"alpha", "beta", "gamma"}
	schema := buildChoiceSchema(opts)

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties")
	}
	selected, ok := props["selected"].(map[string]any)
	if !ok {
		t.Fatal("missing selected property")
	}
	enum, ok := selected["enum"].([]any)
	if !ok {
		t.Fatal("missing enum in selected")
	}
	if len(enum) != len(opts) {
		t.Errorf("expected %d enum values, got %d", len(opts), len(enum))
	}
}
