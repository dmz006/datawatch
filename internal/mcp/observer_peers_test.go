// BL172 (S11) — MCP parity tests for the peer registry tools.

package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestObserverPeer_ToolNames(t *testing.T) {
	s := &Server{}
	cases := []struct {
		want, got string
	}{
		{"observer_peers_list", s.toolObserverPeersList().Name},
		{"observer_peer_get", s.toolObserverPeerGet().Name},
		{"observer_peer_stats", s.toolObserverPeerStats().Name},
		{"observer_peer_register", s.toolObserverPeerRegister().Name},
		{"observer_peer_delete", s.toolObserverPeerDelete().Name},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("tool name = %q, want %q", c.got, c.want)
		}
	}
}

func TestObserverPeerGet_RequiresName(t *testing.T) {
	s := &Server{}
	for _, tool := range []mcpsdk.Tool{
		s.toolObserverPeerGet(),
		s.toolObserverPeerStats(),
		s.toolObserverPeerRegister(),
		s.toolObserverPeerDelete(),
	} {
		ok := false
		for _, r := range tool.InputSchema.Required {
			if r == "name" {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("%s: name must be required, got %v", tool.Name, tool.InputSchema.Required)
		}
	}
}

func TestObserverPeerHandlers_RejectMissingName(t *testing.T) {
	s := &Server{}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		fn   func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	}{
		{"get", s.handleObserverPeerGet},
		{"stats", s.handleObserverPeerStats},
		{"register", s.handleObserverPeerRegister},
		{"delete", s.handleObserverPeerDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := mcpsdk.CallToolRequest{}
			req.Params.Arguments = map[string]any{}
			res, err := tc.fn(ctx, req)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if res == nil || !res.IsError {
				t.Errorf("missing-name should produce IsError result, got %+v", res)
			}
		})
	}
}

func TestObserverPeerHandlers_LoopbackUnavailable(t *testing.T) {
	// webPort=0 → proxyGet/proxyJSON must surface an error rather
	// than panicking. Pass valid args so the handler reaches proxy*.
	s := &Server{}
	ctx := context.Background()
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "p1"}

	if _, err := s.handleObserverPeerGet(ctx, req); err == nil {
		t.Error("get: expected error when webPort=0")
	}
	if _, err := s.handleObserverPeerStats(ctx, req); err == nil {
		t.Error("stats: expected error when webPort=0")
	}
	if _, err := s.handleObserverPeerRegister(ctx, req); err == nil {
		t.Error("register: expected error when webPort=0")
	}
	if _, err := s.handleObserverPeerDelete(ctx, req); err == nil {
		t.Error("delete: expected error when webPort=0")
	}
	if _, err := s.handleObserverPeersList(ctx, mcpsdk.CallToolRequest{}); err == nil {
		t.Error("list: expected error when webPort=0")
	}
}
