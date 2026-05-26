// BL331 parity — MCP tool tests for channel_routing_config_get and channel_routing_config_set.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestBL331_ToolNames(t *testing.T) {
	s := &Server{}
	cases := []struct {
		want string
		got  string
	}{
		{"channel_routing_config_get", s.toolChannelRoutingConfigGet().Name},
		{"channel_routing_config_set", s.toolChannelRoutingConfigSet().Name},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("tool name = %q, want %q", c.got, c.want)
		}
	}
}

func TestBL331_ChannelRoutingConfigGet(t *testing.T) {
	payload := map[string]any{
		"rules": []any{
			map[string]any{
				"channel_pattern": "alerts-*",
				"peer_name":       "peer-alpha",
				"automata_type":   "operational",
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/routing" || r.Method != http.MethodGet {
			http.Error(w, "unexpected", 404)
			return
		}
		json.NewEncoder(w).Encode(payload) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	res, err := s.handleChannelRoutingConfigGet(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, "alerts-*") {
		t.Errorf("expected channel_pattern in response, got: %s", text)
	}
	if !strings.Contains(text, "peer-alpha") {
		t.Errorf("expected peer_name in response, got: %s", text)
	}
}

func TestBL331_ChannelRoutingConfigSet(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/routing" || r.Method != http.MethodPut {
			http.Error(w, "unexpected", 404)
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		json.NewEncoder(w).Encode(map[string]any{"rules": gotBody["rules"]}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	rulesJSON := `[{"channel_pattern":"ops-*","peer_name":"peer-beta"}]`
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"rules_json": rulesJSON}

	res, err := s.handleChannelRoutingConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, "ops-*") {
		t.Errorf("expected channel_pattern in response, got: %s", text)
	}
	if gotBody == nil {
		t.Fatal("server did not receive body")
	}
	rules, _ := gotBody["rules"].([]any)
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestBL331_ChannelRoutingConfigSet_InvalidJSON(t *testing.T) {
	s := &Server{}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"rules_json": "not-json"}

	res, err := s.handleChannelRoutingConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.HasPrefix(text, "Error:") {
		t.Errorf("expected error prefix, got: %s", text)
	}
}

func TestBL331_ChannelRoutingConfigSet_EmptyRulesJSON(t *testing.T) {
	s := &Server{}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"rules_json": ""}

	res, err := s.handleChannelRoutingConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.HasPrefix(text, "Error:") {
		t.Errorf("expected error prefix for empty input, got: %s", text)
	}
}
