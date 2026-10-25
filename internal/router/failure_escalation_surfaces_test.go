// Parity tests for failure escalation over the comm channel. Each test sends a
// chat message through the router, backed by a stand-in daemon that stores the
// override per Automaton and task, then compares what was stored or replied.
package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// escalationStoreDaemon keeps the failure escalation override per "prd|task"
// (raw policy JSON), the last review decision, the PUT /api/config keys and the
// status it serves.
type escalationStoreDaemon struct {
	overrides map[string]string
	resolved  map[string]any
	config    map[string]any
	status    string
}

func newEscalationStoreRouter(t *testing.T) (*Router, *escalationStoreDaemon) {
	t.Helper()
	d := &escalationStoreDaemon{overrides: map[string]string{}, config: map[string]any{}, status: `{}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/autonomous/status":
			_, _ = w.Write([]byte(d.status))
			return
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/set_failure_escalation"):
			prd := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds/"), "/set_failure_escalation")
			task, _ := body["task_id"].(string)
			if body["policy"] == nil {
				delete(d.overrides, prd+"|"+task)
			} else {
				policy, _ := json.Marshal(body["policy"])
				d.overrides[prd+"|"+task] = string(policy)
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resolve_task_review"):
			d.resolved = body
		case r.Method == http.MethodPut && r.URL.Path == "/api/config":
			if body == nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			for k, v := range body {
				d.config[k] = v
			}
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter("h", "g", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, d
}

// TestFailureEscalationCommStoresAutomatonOverride: the set verb with no task
// stores the policy on the Automaton, with each setting it was given.
func TestFailureEscalationCommStoresAutomatonOverride(t *testing.T) {
	r, d := newEscalationStoreRouter(t)
	out := r.HandleTestMessage("autonomous set-failure-escalation p1 enabled=false max_failures=4 steps=pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5")
	if strings.Contains(strings.Join(out, "\n"), "failed") {
		t.Fatalf("reply = %v", out)
	}
	raw, ok := d.overrides["p1|"]
	if !ok {
		t.Fatalf("no Automaton override stored; replies %v", out)
	}
	var got autonomouspkg.FailureEscalationPolicy
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	enabled := false
	want := autonomouspkg.FailureEscalationPolicy{
		Enabled:     &enabled,
		MaxFailures: 4,
		Steps: []autonomouspkg.FailureEscalationStep{
			{Backend: "pi-prompt", Model: "ollama/qwen3.8:27b", Attempts: 2},
			{Backend: "claude-code", Model: "claude-opus-5-5"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored policy = %+v\nwant %+v", got, want)
	}
}

// TestFailureEscalationCommTaskOverrideThenClear: a named task gets its own
// override, the Automaton level is left alone, and clear removes the task one.
func TestFailureEscalationCommTaskOverrideThenClear(t *testing.T) {
	r, d := newEscalationStoreRouter(t)
	r.HandleTestMessage("autonomous set-failure-escalation p1 t1 max_failures=2")
	if _, ok := d.overrides["p1|t1"]; !ok {
		t.Fatalf("no task override stored: %v", d.overrides)
	}
	if _, ok := d.overrides["p1|"]; ok {
		t.Fatal("the Automaton level must stay untouched when a task is named")
	}
	r.HandleTestMessage("autonomous set-failure-escalation p1 t1 clear")
	if _, ok := d.overrides["p1|t1"]; ok {
		t.Errorf("clear left the task override: %v", d.overrides)
	}
}

// TestFailureEscalationCommReviewRetryChangesWorkerAndScope: review-task retry
// with a worker, a narrowed file list and a note reaches the daemon with each.
func TestFailureEscalationCommReviewRetryChangesWorkerAndScope(t *testing.T) {
	r, d := newEscalationStoreRouter(t)
	out := r.HandleTestMessage("autonomous review-task p1 t1 retry backend=claude-code model=claude-opus-5-5 files=a.go,b.go note=narrow it")
	if strings.Contains(strings.Join(out, "\n"), "failed") {
		t.Fatalf("reply = %v", out)
	}
	if d.resolved["task_id"] != "t1" || d.resolved["action"] != "retry" || d.resolved["backend"] != "claude-code" ||
		d.resolved["model"] != "claude-opus-5-5" || d.resolved["note"] != "narrow it" {
		t.Errorf("resolve body = %#v", d.resolved)
	}
	if !reflect.DeepEqual(d.resolved["files"], []any{"a.go", "b.go"}) {
		t.Errorf("files = %#v", d.resolved["files"])
	}
}

// TestFailureEscalationCommStatusListsHeldTasks: the status reply is followed by
// one line per held task, naming the Automaton, the task, its failure count and
// the worker of its last attempt.
func TestFailureEscalationCommStatusListsHeldTasks(t *testing.T) {
	r, d := newEscalationStoreRouter(t)
	d.status = `{"review_needed":[{"automaton_id":"p1","automaton_title":"First","task_id":"t1","task_title":"task one","failure_count":3,"last_backend":"claude-code","last_model":"claude-opus-5-5","held_at":"2026-10-24T09:00:00Z"}]}`
	out := strings.Join(r.HandleTestMessage("autonomous status"), "\n")
	for _, want := range []string{"held for review", "p1", "task t1", "3 failed attempts", "claude-code:claude-opus-5-5", "2026-10-24T09:00:00Z", "review-task"} {
		if !strings.Contains(out, want) {
			t.Errorf("status reply lacks %q:\n%s", want, out)
		}
	}
}

// TestFailureEscalationCommStatusSilentWhenNothingHeld: with no held task the
// status reply is the JSON alone, with no held-for-review line.
func TestFailureEscalationCommStatusSilentWhenNothingHeld(t *testing.T) {
	r, _ := newEscalationStoreRouter(t)
	out := strings.Join(r.HandleTestMessage("autonomous status"), "\n")
	if strings.Contains(out, "held for review") {
		t.Errorf("status with nothing held announces held tasks:\n%s", out)
	}
}

// TestFailureEscalationCommConfigureKeysReachTheConfigPath: configure carries the
// escalation keys to the configure function unchanged, which is the path that
// writes them through PUT /api/config.
func TestFailureEscalationCommConfigureKeysReachTheConfigPath(t *testing.T) {
	r, _ := newEscalationStoreRouter(t)
	got := map[string]string{}
	r.SetConfigureFunc(func(key, value string) error {
		got[key] = value
		return nil
	})
	for _, text := range []string{
		"configure autonomous.failure_escalation.max_failures=4",
		"configure autonomous.failure_escalation.enabled=false",
		"configure autonomous.failure_escalation.steps=pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5",
	} {
		r.HandleTestMessage(text)
	}
	want := map[string]string{
		"autonomous.failure_escalation.max_failures": "4",
		"autonomous.failure_escalation.enabled":      "false",
		"autonomous.failure_escalation.steps":        "pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("configure keys = %v\nwant %v", got, want)
	}
}

// TestFailureEscalationCommHelpNamesVerbs: the autonomous help lists the
// escalation verbs next to set-task-llm, and the configure help (both the help
// and the list text) names the three escalation keys.
func TestFailureEscalationCommHelpNamesVerbs(t *testing.T) {
	r, _ := newEscalationStoreRouter(t)
	help := strings.Join(r.HandleTestMessage("autonomous"), "\n")
	for _, want := range []string{"set-task-llm", "set-failure-escalation <automaton> [task]", "review-task <automaton> <task> retry|cancel"} {
		if !strings.Contains(help, want) {
			t.Errorf("autonomous help lacks %q", want)
		}
	}
}
