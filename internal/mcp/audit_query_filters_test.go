package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/audit"
)

func TestAuditQuery_MCPFilters(t *testing.T) {
	// Definition: the declared arguments are exactly the shared filter set,
	// and the descriptions document the prefix form, the subject filter and
	// the capacity actions.
	tool := (&Server{}).toolAuditQuery()
	want := slices.Clone(audit.QueryParamNames)
	sort.Strings(want)
	if got := AuditQueryToolArgs(); !slices.Equal(got, want) {
		t.Errorf("tool args = %v, want %v", got, want)
	}
	text := tool.Description
	for name, prop := range tool.InputSchema.Properties {
		if m, ok := prop.(map[string]any); ok {
			if d, ok := m["description"].(string); ok {
				text += " " + name + ": " + d
			}
		}
	}
	for _, need := range append([]string{"prefix", "'*'", "subject", "autonomous_budget_*", "autonomous_*", "RFC3339"}, audit.CapacityActions...) {
		if !strings.Contains(text, need) {
			t.Errorf("descriptions lack %q", need)
		}
	}

	// Every argument is forwarded to /api/audit under its own name.
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"count":0,"entries":[]}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"actor": "daemon", "action": "autonomous_budget_*", "subject": "claude",
		"session_id": "s1", "since": "2026-10-22T12:00:00Z", "until": "2026-10-22T13:00:00Z", "limit": float64(5),
	}
	res, err := s.handleAuditQuery(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotPath != "/api/audit" {
		t.Errorf("path = %q", gotPath)
	}
	q, _ := url.ParseQuery(gotQuery)
	for k, v := range map[string]string{
		"actor": "daemon", "action": "autonomous_budget_*", "subject": "claude",
		"session_id": "s1", "since": "2026-10-22T12:00:00Z", "until": "2026-10-22T13:00:00Z", "limit": "5",
	} {
		if q.Get(k) != v {
			t.Errorf("forwarded %s = %q, want %q", k, q.Get(k), v)
		}
	}

	// Unset filters are not forwarded.
	gotQuery = "unset"
	empty := mcpsdk.CallToolRequest{}
	empty.Params.Arguments = map[string]any{}
	if res, err := s.handleAuditQuery(t.Context(), empty); err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotQuery != "" {
		t.Errorf("empty call forwarded %q", gotQuery)
	}

	// An invalid since or until is rejected before any request is made.
	for _, name := range []string{"since", "until"} {
		gotQuery = "untouched"
		bad := mcpsdk.CallToolRequest{}
		bad.Params.Arguments = map[string]any{name: "yesterday"}
		res, err := s.handleAuditQuery(t.Context(), bad)
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Errorf("%s=yesterday was accepted", name)
		}
		if out := resContentText(t, res); !strings.Contains(out, "invalid "+name) {
			t.Errorf("%s error = %q", name, out)
		}
		if gotQuery != "untouched" {
			t.Errorf("%s=yesterday still reached the daemon", name)
		}
	}
}
