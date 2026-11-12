package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// autonomous_config_set must forward scope_lint only when the caller
// supplied it, the same presence-aware way block_on_file_drift is handled.
func TestScopeLintConfigSet_ForwardsOnlyWhenPresent(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{}`))
	})
	for _, want := range []string{"off", "warn", "enforce"} {
		got = nil
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"scope_lint": want})); err != nil {
			t.Fatal(err)
		}
		if v, ok := got["scope_lint"]; !ok || v != want {
			t.Fatalf("scope_lint=%q not forwarded: %v", want, got)
		}
	}
	got = nil
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["scope_lint"]; ok {
		t.Fatalf("scope_lint present when the caller never mentioned it: %v", got)
	}
}

func TestScopeLintConfigSet_SchemaDeclaresEnum(t *testing.T) {
	tool := (&Server{}).toolAutonomousConfigSet()
	prop, ok := tool.InputSchema.Properties["scope_lint"].(map[string]any)
	if !ok {
		t.Fatalf("autonomous_config_set has no scope_lint property: %v", tool.InputSchema.Properties)
	}
	enum, _ := prop["enum"].([]string)
	want := []string{"off", "warn", "enforce"}
	if len(enum) != len(want) {
		t.Fatalf("scope_lint enum = %v, want %v", prop["enum"], want)
	}
	for i := range want {
		if enum[i] != want[i] {
			t.Fatalf("scope_lint enum = %v, want %v", enum, want)
		}
	}
}
