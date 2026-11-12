package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// descendant_stall_seconds — the autonomous_config_set tool schema must list
// the parameter as a number so MCP clients can discover it.
func TestAutonomousConfigSet_DescendantStallSecondsDeclared(t *testing.T) {
	tool := (&Server{}).toolAutonomousConfigSet()
	p, ok := tool.InputSchema.Properties["descendant_stall_seconds"]
	if !ok {
		t.Fatalf("autonomous_config_set does not declare descendant_stall_seconds: %v", tool.InputSchema.Properties)
	}
	if m, _ := p.(map[string]any); m["type"] != "number" {
		t.Errorf("descendant_stall_seconds type = %v, want number", m["type"])
	}
}

// A value, including zero (which the daemon reads as the default), is
// forwarded into the PUT body as a whole number; an absent key is not.
func TestAutonomousConfigSet_DescendantStallSecondsForwarding(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	for _, want := range []float64{1800, 0} {
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"descendant_stall_seconds": want})); err != nil {
			t.Fatal(err)
		}
		if v, ok := got["descendant_stall_seconds"]; !ok || v != want {
			t.Fatalf("descendant_stall_seconds=%v not forwarded: %v", want, got)
		}
	}

	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["descendant_stall_seconds"]; ok {
		t.Fatalf("descendant_stall_seconds present when the caller never mentioned it: %v", got)
	}
}
