package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestSupervisorWatchMCPToolIsListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range tools {
		if tl.Name != "supervisor_watch_list" {
			continue
		}
		found = true
		for _, word := range []string{"shadow", "findings", "last_poll_at", "comparison"} {
			if !strings.Contains(tl.Description, word) {
				t.Errorf("description does not mention %q: %s", word, tl.Description)
			}
		}
	}
	if !found {
		t.Error("tool \"supervisor_watch_list\" is not registered")
	}
	tool := (&Server{}).toolSupervisorWatchList()
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("supervisor_watch_list must be marked read-only")
	}
	if len(tool.InputSchema.Properties) != 0 || len(tool.InputSchema.Required) != 0 {
		t.Errorf("supervisor_watch_list takes no arguments, schema has %v", tool.InputSchema.Properties)
	}
	if got := federation.MCPToolCap["supervisor_watch_list"]; got != federation.CapAutonomousRead {
		t.Errorf("federation capability = %q, want %q", got, federation.CapAutonomousRead)
	}
}

func TestSupervisorWatchMCPEmptyResult(t *testing.T) {
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"mode":"shadow","last_poll_at":"0001-01-01T00:00:00Z","findings":[],"comparison":{"only_watch":[],"only_script":[]}}`))
	})
	res, err := s.handleSupervisorWatchList(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/supervisor-watch" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	var got struct {
		Mode     string            `json:"mode"`
		Findings []json.RawMessage `json:"findings"`
	}
	if err := json.Unmarshal([]byte(automationText(t, res)), &got); err != nil {
		t.Fatalf("result is not the daemon JSON: %v", err)
	}
	if got.Mode != "shadow" || got.Findings == nil || len(got.Findings) != 0 {
		t.Errorf("empty result = %+v", got)
	}
}

func TestSupervisorWatchMCPPopulatedResult(t *testing.T) {
	body := `{"mode":"shadow","last_poll_at":"2026-10-30T10:00:00Z","findings":[` +
		`{"kind":"task_failed","subject":"task one","text":"An Automaton task failed","would_alert":true},` +
		`{"kind":"ci_red","subject":"dmz006/datawatch","text":"CI is red"}],` +
		`"comparison":{"days_agreed":2,"only_watch":["ci_red"],"only_script":[]}}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handleSupervisorWatchList(context.Background(), callReq(map[string]any{}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	if text != body {
		t.Errorf("result differs from the daemon body:\n got %s\nwant %s", text, body)
	}
	for _, want := range []string{`"kind":"task_failed"`, `"kind":"ci_red"`, `"days_agreed":2`} {
		if !strings.Contains(text, want) {
			t.Errorf("result lost %s: %s", want, text)
		}
	}
}

func TestSupervisorWatchMCPRejectsUnknownArguments(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleSupervisorWatchList(context.Background(), callReq(map[string]any{"kind": "ci_red", "repo": "x"}))
	if err != nil {
		t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("an argument must be refused: %+v", res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "unknown argument") || !strings.Contains(text, "kind, repo") {
		t.Errorf("refusal does not name the arguments: %s", text)
	}
	if called {
		t.Error("the daemon was called despite the refused arguments")
	}
}

func TestSupervisorWatchMCPSurfacesDaemonError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
	})
	res, err := s.handleSupervisorWatchList(context.Background(), callReq(nil))
	if err != nil {
		t.Fatalf("protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 503 from the daemon must come back as a tool error: %+v", res)
	}
}
