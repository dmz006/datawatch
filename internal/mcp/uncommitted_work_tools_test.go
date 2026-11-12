package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestUncommittedMCPToolRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range UncommittedWorkToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
	tool := (&Server{}).toolAutonomousUncommittedWork()
	if tool.Name != "autonomous_uncommitted_work" {
		t.Errorf("tool name = %q", tool.Name)
	}
	if hint := tool.Annotations.ReadOnlyHint; hint == nil || !*hint {
		t.Error("autonomous_uncommitted_work must be marked read-only")
	}
	for _, field := range []string{"policy", "scope", "held", "automaton_id", "task_id", "kind",
		"files", "outside_scope", "reason", "question_id", "held_at"} {
		if !strings.Contains(tool.Description, `"`+field+`"`) {
			t.Errorf("description does not name %q", field)
		}
	}
}

func TestUncommittedMCPProxiesRoute(t *testing.T) {
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"policy":"hold","scope":"task_files","held":[` +
			`{"automaton_id":"abcd1234","task_id":"t1","kind":"task","files":["a.go"],"question_id":"q9"}]}`))
	})
	res, err := s.handleAutonomousUncommittedWork(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/uncommitted-work" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s; want the route with no filter", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, `"question_id":"q9"`) {
		t.Errorf("result lost the daemon body: %s", text)
	}

	res, err = s.handleAutonomousUncommittedWork(context.Background(), callReq(map[string]any{"automaton": "abcd1234"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if path != "/api/autonomous/uncommitted-work" || rawQuery != "automaton=abcd1234" {
		t.Errorf("proxied %s?%s; want the automaton query", path, rawQuery)
	}
}

// The autonomous_config_set tool declares the two uncommitted-work arguments
// and forwards them into the nested automation object that
// PUT /api/autonomous/config reads.
func TestUncommittedMCPConfigParams(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	names := configSetArgNames()
	for _, want := range []string{"automation_uncommitted_work_policy", "automation_uncommitted_work_scope"} {
		if !strings.Contains(names, want) {
			t.Errorf("autonomous_config_set does not declare %s", want)
		}
	}
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_uncommitted_work_policy": "warn",
		"automation_uncommitted_work_scope":  "worktree",
	})); err != nil {
		t.Fatalf("config set: %v", err)
	}
	auto, _ := rec.body["automation"].(map[string]any)
	if auto["uncommitted_work_policy"] != "warn" || auto["uncommitted_work_scope"] != "worktree" {
		t.Errorf("automation = %v, want the policy and scope forwarded", rec.body["automation"])
	}
	// Absent arguments send no uncommitted-work keys.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_idle_alert_minutes": 0,
	})); err != nil {
		t.Fatal(err)
	}
	auto, _ = rec.body["automation"].(map[string]any)
	for _, k := range []string{"uncommitted_work_policy", "uncommitted_work_scope"} {
		if _, ok := auto[k]; ok {
			t.Errorf("%s leaked into an idle-only PUT: %v", k, rec.body)
		}
	}
}
