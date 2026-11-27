package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// Claude Code delivers channel notifications only for a server that declares
// the channel capability when it is initialized; without it each one is
// skipped.
func TestInitializeDeclaresTheChannelCapability(t *testing.T) {
	srv := newBridgeServer(&server.Hooks{})
	reply := srv.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result struct {
			Capabilities struct {
				Experimental map[string]any `json:"experimental"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Result.Capabilities.Experimental["claude/channel"]; !ok {
		t.Fatalf("claude/channel is not declared: %s", raw)
	}
	// Relaying permission prompts is a question of who may approve, and is
	// not declared here.
	if _, ok := got.Result.Capabilities.Experimental["claude/channel/permission"]; ok {
		t.Fatal("the permission relay capability is declared")
	}
}
