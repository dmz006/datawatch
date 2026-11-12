// BL412 — self_config_audit: the MCP read view.

package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestSelfConfigAudit_MCPProxy(t *testing.T) {
	// The tool is a limit-only read: the description documents the
	// surfaces, the allowed+denied scope and the newest-first order.
	tool := (&Server{}).toolSelfConfigAudit()
	if tool.Name != "self_config_audit" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if _, ok := tool.InputSchema.Properties["limit"]; !ok {
		t.Errorf("tool lacks the limit argument")
	}
	text := tool.Description
	for _, need := range []string{"newest-first", "allowed", "denied", "comm"} {
		if !strings.Contains(text, need) {
			t.Errorf("description lacks %q: %s", need, text)
		}
	}

	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"count":0,"entries":[],"path":"/x.jsonl"}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": float64(5)}
	res, err := s.handleSelfConfigAudit(t.Context(), req)
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotPath != "/api/audit/self-config" {
		t.Errorf("path = %q", gotPath)
	}
	q, _ := url.ParseQuery(gotQuery)
	if q.Get("limit") != "5" {
		t.Errorf("forwarded limit = %q, want 5", q.Get("limit"))
	}

	// No limit → no query string (the daemon applies its default).
	gotQuery = "unset"
	empty := mcpsdk.CallToolRequest{}
	empty.Params.Arguments = map[string]any{}
	if res, err := s.handleSelfConfigAudit(t.Context(), empty); err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotQuery != "" {
		t.Errorf("empty call forwarded query %q, want none", gotQuery)
	}
}
