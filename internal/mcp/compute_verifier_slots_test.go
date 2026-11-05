package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TestVerifyNodeToolsAdvertiseVerifierSettings checks both node tools carry
// the three verifier settings as strings, with the wording that makes the
// null semantics clear.
func TestVerifyNodeToolsAdvertiseVerifierSettings(t *testing.T) {
	s := &Server{}
	tools := map[string]mcpsdk.Tool{
		"compute_node_add":    s.toolComputeNodeAdd(),
		"compute_node_update": s.toolComputeNodeUpdate(),
	}
	for name, tool := range tools {
		for _, k := range []string{"verifier_slots", "verifier_min_free_mem_mb", "verifier_context_mb"} {
			raw, ok := tool.InputSchema.Properties[k]
			if !ok {
				t.Errorf("%s: missing the %s argument", name, k)
				continue
			}
			prop, _ := raw.(map[string]any)
			if prop["type"] != "string" {
				t.Errorf("%s: %s type = %v, want string", name, k, prop["type"])
			}
			if d, _ := prop["description"].(string); !strings.Contains(d, "verifier") {
				t.Errorf("%s: %s description = %q, want the verifier wording", name, k, d)
			}
		}
	}
	if !strings.Contains(s.toolComputeNodeDetail().Description, "suggested_verifier_slots") {
		t.Errorf("compute_node_detail description is missing the suggested_verifier_slots figure")
	}
}

// TestNodeAddVerifierSettingsLandInBody checks the three settings arrive as
// JSON numbers, and an explicit zero is kept distinct from "unset".
func TestNodeAddVerifierSettingsLandInBody(t *testing.T) {
	s, d := newComputeTLSServer(t)
	args := map[string]any{
		"name": "gpu-1", "kind": "ollama",
		"verifier_slots": "2", "verifier_min_free_mem_mb": "512", "verifier_context_mb": "128",
	}
	if _, err := s.handleComputeNodeAddMCP(context.Background(), computeTLSReq(args)); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := d.body["verifier_slots"]; got != float64(2) {
		t.Errorf("verifier_slots = %v, want the number 2 (explicit zero must stay 0, never \u003c 0)", got)
	}
	if got := d.body["verifier_min_free_mem_mb"]; got != float64(512) {
		t.Errorf("verifier_min_free_mem_mb = %v, want 512", got)
	}
	if got := d.body["verifier_context_mb"]; got != float64(128) {
		t.Errorf("verifier_context_mb = %v, want 128", got)
	}

	// Explicit 0: still a number in the body — "no verifier slots".
	s, d = newComputeTLSServer(t)
	args = map[string]any{"name": "cpu-only", "kind": "ollama", "verifier_slots": "0"}
	if _, err := s.handleComputeNodeAddMCP(context.Background(), computeTLSReq(args)); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := d.body["verifier_slots"]; got != float64(0) {
		t.Errorf("verifier_slots = %v, want the number 0 (not absent)", got)
	}
}

// TestNodeUpdateOmittedVerifierSettingsAreCleared checks empty strings stay
// out of the body, so a replace with no settings clears any earlier override.
func TestNodeUpdateOmittedVerifierSettingsAreCleared(t *testing.T) {
	s, d := newComputeTLSServer(t)
	args := map[string]any{"name": "gpu-1", "kind": "ollama", "tags": "a"}
	if _, err := s.handleComputeNodeUpdateMCP(context.Background(), computeTLSReq(args)); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, k := range []string{"verifier_slots", "verifier_min_free_mem_mb", "verifier_context_mb"} {
		if _, present := d.body[k]; present {
			t.Errorf("%s was sent although it was not set: %s", k, d.raw)
		}
	}
}

// TestNodeVerifierValidationErrorsSurfaceUnchanged checks the server's
// Verify() wording reaches the MCP caller as is for each negative setting.
func TestNodeVerifierValidationErrorsSurfaceUnchanged(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"negative verifier_slots", map[string]any{"name": "gpu-1", "kind": "ollama", "verifier_slots": "-3"},
			"compute node: verifier_slots -3 must be >= 0"},
		{"negative min free mem", map[string]any{"name": "gpu-1", "kind": "ollama", "verifier_min_free_mem_mb": "-64"},
			"compute node: verifier_min_free_mem_mb -64 must be >= 0"},
		{"negative context", map[string]any{"name": "gpu-1", "kind": "ollama", "verifier_context_mb": "-1"},
			"compute node: verifier_context_mb -1 must be >= 0"},
	}
	for _, tc := range cases {
		for _, h := range []string{"add", "update"} {
			t.Run(tc.name+"/"+h, func(t *testing.T) {
				s, _ := newComputeTLSServer(t)
				handler := s.handleComputeNodeAddMCP
				if h == "update" {
					handler = s.handleComputeNodeUpdateMCP
				}
				_, err := handler(context.Background(), computeTLSReq(tc.args))
				if err == nil {
					t.Fatal("expected the server's validation error, got nil")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
				}
			})
		}
	}
}
