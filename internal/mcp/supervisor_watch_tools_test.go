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
	trackSessionManager(t, mgr)
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

func TestSupervisorWatchStatsMCPTool(t *testing.T) {
	tool := (&Server{}).toolSupervisorWatchStats()
	if tool.Name != "supervisor_watch_stats" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("supervisor_watch_stats must be marked read-only")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("takes no arguments, schema has %v", tool.InputSchema.Properties)
	}
	if got := federation.MCPToolCap["supervisor_watch_stats"]; got != federation.CapAutonomousRead {
		t.Errorf("federation capability = %q, want %q", got, federation.CapAutonomousRead)
	}

	var path, method string
	body := `{"mode":"shadow","last_poll_age_seconds":5,"findings_open":1}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handleSupervisorWatchStats(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/supervisor-watch/stats" {
		t.Errorf("proxied %s %s", method, path)
	}
	if text := automationText(t, res); text != body {
		t.Errorf("result differs from the daemon body: %s", text)
	}

	res, err = s.handleSupervisorWatchStats(context.Background(), callReq(map[string]any{"repo": "x"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("an argument must be refused: err=%v res=%+v", err, res)
	}

	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
	})
	res, err = s.handleSupervisorWatchStats(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 503 must come back as a tool error: err=%v res=%+v", err, res)
	}
}

// TestSupervisorWatchToolsPriorityBands — the per-band wait stats ride
// through both read tools: both responses carry the priority_bands key
// (an empty list where no band waits anything) and the populated rows
// come back byte-for-byte, high before medium before low.
func TestSupervisorWatchToolsPriorityBands(t *testing.T) {
	const high = `{"band":"high","priority_from":80,"priority_to":100,"waiting":2,"oldest_waiting_minutes":41,"stalled":1}`
	const low = `{"band":"low","priority_from":0,"priority_to":49,"waiting":1,"oldest_waiting_minutes":5,"stalled":0}`

	// supervisor_watch_list: the status object carries priority_bands
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"shadow","findings":[],"priority_bands":[` + high + `,` + low + `]}`))
	})
	res, err := s.handleSupervisorWatchList(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("list: err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	for _, want := range []string{`"priority_bands":[`, high, low} {
		if !strings.Contains(text, want) {
			t.Errorf("supervisor_watch_list result lost %s: %s", want, text)
		}
	}

	// an empty band list still carries the key, so a caller always gets
	// a list, never the absence of one
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"shadow","findings":[],"priority_bands":[]}`))
	})
	res, err = s.handleSupervisorWatchList(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("list (empty bands): err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, `"priority_bands":[]`) {
		t.Errorf("an empty band list must keep its key: %s", text)
	}

	// supervisor_watch_stats: the numeric view carries the same key and
	// rows
	statsBody := `{"mode":"shadow","last_poll_age_seconds":5,"findings_open":1,"priority_bands":[` + high + `]}`
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(statsBody))
	})
	res, err = s.handleSupervisorWatchStats(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("stats: err=%v res=%+v", err, res)
	}
	text = automationText(t, res)
	if !strings.Contains(text, `"priority_bands":[`) || !strings.Contains(text, high) {
		t.Errorf("supervisor_watch_stats result lost the bands: %s", text)
	}
	if text != statsBody {
		t.Errorf("result differs from the daemon body: %s", text)
	}
}

func TestPriorityStallsMCPTool(t *testing.T) {
	tool := (&Server{}).toolPriorityStalls()
	if tool.Name != "priority_stalls" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("priority_stalls must be marked read-only")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("takes no arguments, schema has %v", tool.InputSchema.Properties)
	}
	if got := federation.MCPToolCap["priority_stalls"]; got != federation.CapAutonomousRead {
		t.Errorf("federation capability = %q, want %q", got, federation.CapAutonomousRead)
	}
	found := false
	for _, n := range SupervisorWatchToolNames() {
		found = found || n == "priority_stalls"
	}
	if !found {
		t.Error("priority_stalls missing from SupervisorWatchToolNames")
	}

	var path, method string
	body := `{"stalls":[{"automaton_id":"a1","blocker_code":"held","blocker_text":"on hold","action":"release the hold"}]}`
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handlePriorityStalls(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/priority-stalls" {
		t.Errorf("proxied %s %s", method, path)
	}
	if text := automationText(t, res); text != body {
		t.Errorf("result differs from the daemon body: %s", text)
	}
	res, err = s.handlePriorityStalls(context.Background(), callReq(map[string]any{"x": 1}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("an argument must be refused: err=%v res=%+v", err, res)
	}
}
