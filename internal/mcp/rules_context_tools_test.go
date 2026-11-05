package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

type rulesContextFake struct {
	mu     sync.Mutex
	cfg    string
	puts   []string
	query  string
	gotGet string
}

func newRulesContextFakeServer(t *testing.T, status int) (*Server, *rulesContextFake) {
	t.Helper()
	f := &rulesContextFake{cfg: `{"rules_file":"RULES.md","context_files":["A.md"],"digest_mode":"off","projects":[{"project_dir":"/srv/x","rules_file":"X.md"}]}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != "/api/autonomous/rules-context" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			http.Error(w, "refused", status)
			return
		}
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			f.puts = append(f.puts, string(raw))
			f.query = r.URL.RawQuery
			f.cfg = string(raw)
		} else {
			f.gotGet = r.URL.RawQuery
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"config":` + f.cfg + `,"policy":{"rules_file":"RULES.md","context_files":[],"digest_mode":"off","digest_max_bytes":0}}`))
	}))
	t.Cleanup(srv.Close)
	s := &Server{}
	s.SetWebPort(portOf(t, srv.URL))
	return s, f
}

func portOf(t *testing.T, u string) int {
	t.Helper()
	n := 0
	for _, c := range u[strings.LastIndex(u, ":")+1:] {
		n = n*10 + int(c-'0')
	}
	return n
}

func rulesCallReq(name string, args map[string]any) mcpsdk.CallToolRequest {
	return mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Name: name, Arguments: args}}
}

func rulesResultText(t *testing.T, r *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		return ""
	}
	tc, ok := r.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content is %T", r.Content[0])
	}
	return tc.Text
}

func TestRulesContextSettingsMCP_ReadPassesFilters(t *testing.T) {
	s, f := newRulesContextFakeServer(t, http.StatusOK)
	res, err := s.handleRulesContextStatus(context.Background(), rulesCallReq("rules_context_status", map[string]any{"prd": "p1", "project_dir": "/srv/x"}))
	if err != nil || res.IsError {
		t.Fatalf("read failed: %v %+v", err, res)
	}
	if f.gotGet != "prd=p1&project_dir=%2Fsrv%2Fx" {
		t.Errorf("query = %q", f.gotGet)
	}
	if !strings.Contains(rulesResultText(t, res), "RULES.md") {
		t.Errorf("body = %q", rulesResultText(t, res))
	}
	tool := s.toolRulesContextStatus()
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("rules_context_status must be read-only")
	}
}

func TestRulesContextSettingsMCP_SetMergesAndWrites(t *testing.T) {
	s, f := newRulesContextFakeServer(t, http.StatusOK)
	res, err := s.handleRulesContextSet(context.Background(), rulesCallReq("rules_context_set", map[string]any{
		"digest_mode": "ALL", "digest_max_bytes": float64(2048), "context_files": []any{"B.md", "C.md"},
	}))
	if err != nil || res.IsError {
		t.Fatalf("set failed: %v %s", err, rulesResultText(t, res))
	}
	if len(f.puts) != 1 {
		t.Fatalf("%d PUTs", len(f.puts))
	}
	for _, want := range []string{`"rules_file":"RULES.md"`, `"digest_mode":"all"`, `"digest_max_bytes":2048`, `"B.md"`, `"project_dir":"/srv/x"`} {
		if !strings.Contains(f.puts[0], want) {
			t.Errorf("PUT body lacks %s: %s", want, f.puts[0])
		}
	}
	// Per-project override.
	res, err = s.handleRulesContextSet(context.Background(), rulesCallReq("rules_context_set", map[string]any{"rules_file": "Y.md", "project_dir": "/srv/y"}))
	if err != nil || res.IsError {
		t.Fatalf("override failed: %v %s", err, rulesResultText(t, res))
	}
	if !strings.Contains(f.puts[1], `"project_dir":"/srv/y"`) || !strings.Contains(f.puts[1], `"rules_file":"Y.md"`) || !strings.Contains(f.puts[1], `"digest_mode":"all"`) {
		t.Errorf("override body: %s", f.puts[1])
	}
	if f.query != "project_dir=%2Fsrv%2Fy" {
		t.Errorf("query = %q", f.query)
	}
}

func TestRulesContextSettingsMCP_SetRejectsBadValues(t *testing.T) {
	s, f := newRulesContextFakeServer(t, http.StatusOK)
	for name, args := range map[string]map[string]any{
		"bad mode":      {"digest_mode": "sometimes"},
		"absolute path": {"rules_file": "/etc/passwd"},
		"escape":        {"context_files": []any{"../x"}},
		"too small":     {"digest_max_bytes": float64(10)},
		"fraction":      {"digest_max_bytes": 1.5},
		"nothing":       {},
	} {
		res, err := s.handleRulesContextSet(context.Background(), rulesCallReq("rules_context_set", args))
		if err != nil || res == nil || !res.IsError {
			t.Errorf("%s: want a tool error, got %v %+v", name, err, res)
		}
	}
	if len(f.puts) != 0 {
		t.Errorf("a rejected value must not write: %v", f.puts)
	}
	s2, _ := newRulesContextFakeServer(t, http.StatusServiceUnavailable)
	res, _ := s2.handleRulesContextStatus(context.Background(), rulesCallReq("rules_context_status", nil))
	if !res.IsError {
		t.Error("an upstream 503 must be a tool error")
	}
}

func TestRulesContextSettingsMCP_NamesAndCaps(t *testing.T) {
	s := &Server{}
	if s.toolRulesContextStatus().Name != "rules_context_status" || s.toolRulesContextSet().Name != "rules_context_set" {
		t.Error("tool names")
	}
	if len(RulesContextToolNames()) != 2 {
		t.Error("RulesContextToolNames must list both tools")
	}
	if federation.MCPToolCap["rules_context_status"] != federation.CapAutonomousRead || federation.MCPToolCap["rules_context_set"] != federation.CapAutonomousWrite {
		t.Error("capabilities")
	}
	for _, p := range []string{"rules_file", "context_files", "digest_mode", "digest_max_bytes", "project_dir"} {
		if _, ok := s.toolRulesContextSet().InputSchema.Properties[p]; !ok {
			t.Errorf("rules_context_set lacks %s", p)
		}
	}
}
