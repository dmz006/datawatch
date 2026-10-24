// Parity tests for failure escalation over MCP. Each test drives an MCP tool
// against an in-memory stand-in for the daemon and then compares what the
// stand-in stored for the Automaton, so a tool that sends the wrong body or
// route fails here even when its request looks right.
package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// escalationDaemon stands in for the daemon's REST routes the escalation tools
// use: it stores the failure escalation override per Automaton and task, keeps
// the resolve requests it receives, serves a fixed status and stores PUT
// /api/config keys.
type escalationDaemon struct {
	mu        sync.Mutex
	overrides map[string]any // "prd|task" -> stored policy
	resolved  []map[string]any
	config    map[string]any
	status    string
}

func newEscalationDaemon(t *testing.T) (*escalationDaemon, *Server) {
	t.Helper()
	d := &escalationDaemon{overrides: map[string]any{}, config: map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(srv.Close)
	return d, &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: true}}
}

func (d *escalationDaemon) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	switch {
	case r.Method == http.MethodPut && r.URL.Path == "/api/config":
		if body == nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		for k, v := range body {
			d.config[k] = v
		}
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/set_failure_escalation"):
		prd := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds/"), "/set_failure_escalation")
		task, _ := body["task_id"].(string)
		if body["policy"] == nil {
			delete(d.overrides, prd+"|"+task)
		} else {
			d.overrides[prd+"|"+task] = body["policy"]
		}
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resolve_task_review"):
		d.resolved = append(d.resolved, body)
	case r.Method == http.MethodGet && r.URL.Path == "/api/autonomous/status":
		if d.status == "" {
			d.status = `{}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(d.status))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (d *escalationDaemon) override(key string) (any, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.overrides[key]
	return v, ok
}

// TestFailureEscalationMCPSetStoresPolicyOnTask: the set tool stores the policy
// on the named task of the Automaton, with every field it was given.
func TestFailureEscalationMCPSetStoresPolicyOnTask(t *testing.T) {
	d, s := newEscalationDaemon(t)
	req := subUpdateReq("autonomous_prd_set_failure_escalation", map[string]any{
		"id":      "p1",
		"task_id": "t1",
		"policy": map[string]any{
			"enabled":      false,
			"max_failures": 4,
			"steps": []any{
				map[string]any{"backend": "pi-prompt", "model": "ollama/qwen3.8:27b", "attempts": 2},
				map[string]any{"backend": "claude-code", "model": "claude-opus-5-5", "effort": "high"},
			},
		},
	})
	if _, err := s.handleAutonomousPRDSetFailureEscalation(context.Background(), req); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok := d.override("p1|t1")
	if !ok {
		t.Fatal("no override stored for p1/t1")
	}
	want := map[string]any{
		"enabled":      false,
		"max_failures": float64(4),
		"steps": []any{
			map[string]any{"backend": "pi-prompt", "model": "ollama/qwen3.8:27b", "attempts": float64(2)},
			map[string]any{"backend": "claude-code", "model": "claude-opus-5-5", "effort": "high"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored policy = %#v\nwant %#v", got, want)
	}
	if _, ok := d.override("p1|"); ok {
		t.Error("the Automaton level must stay untouched when a task is named")
	}
}

// TestFailureEscalationMCPClearRemovesOverride: clear=true removes the override
// the set stored, at the same level.
func TestFailureEscalationMCPClearRemovesOverride(t *testing.T) {
	d, s := newEscalationDaemon(t)
	set := subUpdateReq("autonomous_prd_set_failure_escalation", map[string]any{
		"id":     "p1",
		"policy": map[string]any{"max_failures": 2},
	})
	if _, err := s.handleAutonomousPRDSetFailureEscalation(context.Background(), set); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok := d.override("p1|"); !ok {
		t.Fatal("set did not store the Automaton override")
	}
	clr := subUpdateReq("autonomous_prd_set_failure_escalation", map[string]any{"id": "p1", "clear": true})
	if _, err := s.handleAutonomousPRDSetFailureEscalation(context.Background(), clr); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := d.override("p1|"); ok {
		t.Error("clear left the Automaton override in place")
	}
}

// TestFailureEscalationMCPResolveRetryCarriesChanges: a retry with a worker,
// narrowed files and a note reaches the daemon with each of them.
func TestFailureEscalationMCPResolveRetryCarriesChanges(t *testing.T) {
	d, s := newEscalationDaemon(t)
	req := subUpdateReq("autonomous_prd_resolve_task_review", map[string]any{
		"id":      "p1",
		"task_id": "t1",
		"action":  "retry",
		"backend": "claude-code",
		"model":   "claude-opus-5-5",
		"effort":  "high",
		"files":   []any{"internal/a.go", "internal/b.go"},
		"note":    "try the narrower scope",
	})
	if _, err := s.handleAutonomousPRDResolveTaskReview(context.Background(), req); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(d.resolved) != 1 {
		t.Fatalf("resolve requests = %d, want 1", len(d.resolved))
	}
	got := d.resolved[0]
	if got["task_id"] != "t1" || got["action"] != "retry" || got["backend"] != "claude-code" ||
		got["model"] != "claude-opus-5-5" || got["effort"] != "high" || got["note"] != "try the narrower scope" {
		t.Errorf("resolve body = %#v", got)
	}
	if !reflect.DeepEqual(got["files"], []any{"internal/a.go", "internal/b.go"}) {
		t.Errorf("files = %#v", got["files"])
	}
}

// TestFailureEscalationMCPStatusDescribesReviewNeeded: the status tool says in
// its description that the review_needed array exists and how to act on it.
func TestFailureEscalationMCPStatusDescribesReviewNeeded(t *testing.T) {
	desc := (&Server{}).toolAutonomousStatus().Description
	for _, want := range []string{"review_needed", "automaton_id", "task_id", "failure_count", "last_backend", "held_at", "autonomous_prd_resolve_task_review"} {
		if !strings.Contains(desc, want) {
			t.Errorf("autonomous_status description omits %q", want)
		}
	}
}

// TestFailureEscalationMCPStatusListsHeldTasks: the status result carries the
// held tasks of every Automaton under review_needed, unchanged from the daemon.
func TestFailureEscalationMCPStatusListsHeldTasks(t *testing.T) {
	d, s := newEscalationDaemon(t)
	d.status = `{"running":true,"review_needed":[{"automaton_id":"p1","automaton_title":"First","task_id":"t1","task_title":"task one","failure_count":3,"last_backend":"claude-code","last_model":"claude-opus-5-5","held_at":"2026-10-24T09:00:00Z"},{"automaton_id":"p2","automaton_title":"Second","task_id":"t9","failure_count":4,"last_backend":"pi-prompt","held_at":"2026-10-24T10:00:00Z"}]}`
	res, err := s.handleAutonomousStatus(context.Background(), subUpdateReq("autonomous_status", nil))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var st struct {
		ReviewNeeded []map[string]any `json:"review_needed"`
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &st); err != nil {
		t.Fatalf("status result is not JSON: %v\n%s", err, text)
	}
	if len(st.ReviewNeeded) != 2 {
		t.Fatalf("review_needed has %d rows, want 2: %s", len(st.ReviewNeeded), text)
	}
	if st.ReviewNeeded[0]["automaton_id"] != "p1" || st.ReviewNeeded[0]["task_id"] != "t1" || st.ReviewNeeded[0]["failure_count"] != float64(3) {
		t.Errorf("first held row = %#v", st.ReviewNeeded[0])
	}
}

// TestFailureEscalationMCPConfigSetAcceptsEscalationKeys: the generic config
// tool passes autonomous.failure_escalation.* keys through to PUT /api/config,
// with a number, a bool and the compact steps text, and the stored config
// carries each value.
func TestFailureEscalationMCPConfigSetAcceptsEscalationKeys(t *testing.T) {
	d, s := newEscalationDaemon(t)
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.failure_escalation.max_failures", "4", float64(4)},
		{"autonomous.failure_escalation.enabled", "false", false},
		{"autonomous.failure_escalation.steps", "pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5", "pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5"},
	}
	for _, c := range cases {
		req := subUpdateReq("config_set", map[string]any{"key": c.key, "value": c.value})
		res, err := s.handleConfigSet(context.Background(), req)
		if err != nil {
			t.Fatalf("config_set %s: %v", c.key, err)
		}
		if text := res.Content[0].(mcpsdk.TextContent).Text; strings.Contains(text, "error") || strings.Contains(text, "denied") {
			t.Fatalf("config_set %s refused: %s", c.key, text)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range cases {
		if got := d.config[c.key]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("stored %s = %#v, want %#v", c.key, got, c.want)
		}
	}
}

// TestFailureEscalationMCPToolsRegistered: both escalation tools are in the
// catalog a real server exposes, so they reach stdio, SSE and REST clients.
func TestFailureEscalationMCPToolsRegistered(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"autonomous_prd_set_failure_escalation", "autonomous_prd_resolve_task_review", "autonomous_status", "config_set"} {
		if !names[want] {
			t.Errorf("MCP catalog lacks %s", want)
		}
	}
}
