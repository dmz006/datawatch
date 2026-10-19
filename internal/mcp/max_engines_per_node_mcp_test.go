package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// A set through autonomous_config_set must reach the REST PUT body, and a
// get through autonomous_config_get must return what the REST side holds.
// The fake REST keeps the last PUT body and serves it back on GET, so this
// is a true round trip at the MCP layer.
func TestAutonomousConfig_MaxEnginesPerNodeRoundTrip(t *testing.T) {
	stored := map[string]any{}
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(stored)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		for k, v := range body {
			stored[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_engines_per_node": 2})); err != nil {
		t.Fatal(err)
	}
	if stored["max_engines_per_node"] != float64(2) {
		t.Fatalf("PUT body lacks max_engines_per_node=2: %v", stored)
	}

	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, `"max_engines_per_node":2`) {
		t.Fatalf("autonomous_config_get output lacks max_engines_per_node:2: %s", text)
	}
}

// A zero is forwarded so the REST write can clamp it to 1; an absent key is
// not forwarded at all.
func TestAutonomousConfigSet_MaxEnginesPerNodeForwarding(t *testing.T) {
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"max_engines_per_node": 0})); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["max_engines_per_node"]; !ok || v != float64(0) {
		t.Fatalf("max_engines_per_node=0 not forwarded for REST to clamp: %v", got)
	}

	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["max_engines_per_node"]; ok {
		t.Fatalf("max_engines_per_node present when the caller never mentioned it: %v", got)
	}
}
