package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
)

// Over MCP no caller can create or enable a filter that types into sessions,
// or turn the daemon switch on.
func TestMCPCannotMakeAFilterType(t *testing.T) {
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/filters" {
			_, _ = w.Write([]byte(`[{"id":"typ1","action":"send_input"},{"id":"al01","action":"alert"}]`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	// Self-config is allowed: the refusals below are the filter rules' own.
	s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
	call := func(h func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error), args map[string]any) {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = args
		if _, err := h(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []string{"send_input", "schedule"} {
		call(s.handleFilterAdd, map[string]any{"pattern": "p", "action": a, "value": "v"})
	}
	call(s.handleFilterToggle, map[string]any{"id": "typ1", "enabled": true})
	for _, v := range []string{"true", "1", `"yes"`} {
		call(s.handleConfigSet, map[string]any{"key": "session.input_filters_enabled", "value": v})
	}
	if len(writes) != 0 {
		t.Fatalf("refused calls reached the daemon: %v", writes)
	}
	// Turning things off, and non-typing filters, still work.
	call(s.handleFilterToggle, map[string]any{"id": "typ1", "enabled": false})
	call(s.handleFilterToggle, map[string]any{"id": "al01", "enabled": true})
	call(s.handleConfigSet, map[string]any{"key": "session.input_filters_enabled", "value": "false"})
	if len(writes) != 3 {
		t.Fatalf("allowed calls = %v", writes)
	}
}
