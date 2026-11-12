package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

func traceToolServer(t *testing.T, status int, body string) (*Server, *string) {
	t.Helper()
	got := new(string)
	return capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		*got = r.Method + " " + r.URL.EscapedPath() + "?" + r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}), got
}

func TestTraceTools_SchemaAndRegistration(t *testing.T) {
	s := &Server{}
	tools := map[string][]string{
		"trace_session":   {"session"},
		"trace_automaton": {"automaton"},
		"trace_rollup":    nil,
		"trace_coverage":  nil,
	}
	built := map[string]struct {
		name     string
		required []string
		readOnly bool
		desc     string
	}{}
	for _, tl := range []struct {
		name string
		get  func() (string, []string, bool, string)
	}{
		{"trace_session", func() (string, []string, bool, string) {
			x := s.toolTraceSession()
			return x.Name, x.InputSchema.Required, x.Annotations.ReadOnlyHint != nil && *x.Annotations.ReadOnlyHint, x.Description
		}},
		{"trace_automaton", func() (string, []string, bool, string) {
			x := s.toolTraceAutomaton()
			return x.Name, x.InputSchema.Required, x.Annotations.ReadOnlyHint != nil && *x.Annotations.ReadOnlyHint, x.Description
		}},
		{"trace_rollup", func() (string, []string, bool, string) {
			x := s.toolTraceRollup()
			return x.Name, x.InputSchema.Required, x.Annotations.ReadOnlyHint != nil && *x.Annotations.ReadOnlyHint, x.Description
		}},
		{"trace_coverage", func() (string, []string, bool, string) {
			x := s.toolTraceCoverage()
			return x.Name, x.InputSchema.Required, x.Annotations.ReadOnlyHint != nil && *x.Annotations.ReadOnlyHint, x.Description
		}},
	} {
		n, req, ro, d := tl.get()
		built[tl.name] = struct {
			name     string
			required []string
			readOnly bool
			desc     string
		}{n, req, ro, d}
	}
	for name, wantReq := range tools {
		b, ok := built[name]
		if !ok || b.name != name {
			t.Fatalf("%s: name = %q", name, b.name)
		}
		if strings.Join(b.required, ",") != strings.Join(wantReq, ",") {
			t.Errorf("%s required = %v, want %v", name, b.required, wantReq)
		}
		if !b.readOnly {
			t.Errorf("%s must be read-only", name)
		}
		if strings.Contains(b.desc, "PRD") {
			t.Errorf("%s description must say Automaton, not PRD", name)
		}
	}

	srv := New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), Options{Version: "test"})
	listed, err := srv.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tl := range listed {
		seen[tl.Name] = true
	}
	for name := range tools {
		if !seen[name] {
			t.Errorf("%s is not registered", name)
		}
	}
}

func TestTraceTools_ProxyRoutesAndQuery(t *testing.T) {
	s, got := traceToolServer(t, http.StatusOK, `{"ok":true}`)
	ctx := context.Background()
	res, err := s.handleTraceSession(ctx, callReq(map[string]any{
		"session": "host/s1", "tool": "Read", "backend": "pi", "kind": "tool_call",
		"errors": true, "limit": float64(5), "loop_threshold": float64(3),
	}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if want := "GET /api/trace/session/host%2Fs1?backend=pi&errors=true&kind=tool_call&limit=5&loop_threshold=3&tool=Read"; *got != want {
		t.Errorf("proxied %q, want %q", *got, want)
	}
	if automationText(t, res) != `{"ok":true}` {
		t.Errorf("body not relayed: %q", automationText(t, res))
	}
	if res, _ = s.handleTraceAutomaton(ctx, callReq(map[string]any{"automaton": "a1"})); res.IsError || *got != "GET /api/trace/automaton/a1?" {
		t.Errorf("automaton proxied %q", *got)
	}
	if res, _ = s.handleTraceRollup(ctx, callReq(map[string]any{"dimension": "backend", "key": "pi"})); res.IsError || *got != "GET /api/trace/rollup?dimension=backend&key=pi" {
		t.Errorf("rollup proxied %q", *got)
	}
	if res, _ = s.handleTraceCoverage(ctx, callReq(map[string]any{"backend": "ollama"})); res.IsError || *got != "GET /api/trace/coverage?backend=ollama" {
		t.Errorf("coverage proxied %q", *got)
	}
}

func TestTraceStatsTool_SchemaCapabilityAndProxy(t *testing.T) {
	tool := (&Server{}).toolTraceStats()
	if tool.Name != "trace_stats" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("trace_stats must be read-only")
	}
	if len(tool.InputSchema.Properties) != 0 || len(tool.InputSchema.Required) != 0 {
		t.Errorf("trace_stats takes no parameters, got %v", tool.InputSchema.Properties)
	}
	if strings.Contains(tool.Description, "PRD") {
		t.Error("trace_stats description must say Automaton, not PRD")
	}
	if got := federation.MCPToolCap["trace_stats"]; got != federation.CapAnalyticsRead {
		t.Errorf("capability = %q, want %q", got, federation.CapAnalyticsRead)
	}

	srv := New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), Options{Version: "test"})
	listed, err := srv.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tl := range listed {
		found = found || tl.Name == "trace_stats"
	}
	if !found {
		t.Error("trace_stats is not registered")
	}

	s, got := traceToolServer(t, http.StatusOK, `{"sessions":2,"events":9,"error_events":1}`)
	res, err := s.handleTraceStats(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if *got != "GET /api/trace/stats?" {
		t.Errorf("proxied %q, want GET /api/trace/stats", *got)
	}
	if automationText(t, res) != `{"sessions":2,"events":9,"error_events":1}` {
		t.Errorf("body not relayed: %q", automationText(t, res))
	}
}

func TestTraceStatsTool_ReportsAFailedRead(t *testing.T) {
	s, _ := traceToolServer(t, http.StatusInternalServerError, "boom")
	if res, err := s.handleTraceStats(context.Background(), callReq(nil)); err != nil || res == nil || !res.IsError {
		t.Fatalf("err=%v res=%+v, want a tool error", err, res)
	}
}

func TestTraceTools_MissingIDAndRESTError(t *testing.T) {
	s, got := traceToolServer(t, http.StatusNotFound, "no trace for session zz")
	ctx := context.Background()
	if res, _ := s.handleTraceSession(ctx, callReq(map[string]any{"session": "  "})); res == nil || !res.IsError {
		t.Error("blank session must be a tool error")
	}
	if res, _ := s.handleTraceAutomaton(ctx, callReq(nil)); res == nil || !res.IsError {
		t.Error("missing automaton must be a tool error")
	}
	if *got != "" {
		t.Errorf("an invalid call reached the daemon: %q", *got)
	}
	if res, _ := s.handleTraceSession(ctx, callReq(map[string]any{"session": "zz"})); res == nil || !res.IsError {
		t.Error("a REST 404 must be a tool error")
	}
}
