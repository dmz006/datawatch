// Parity tests for failure escalation on the CLI. Each test runs a command
// against a stand-in daemon that stores the override per Automaton and task, and
// then compares what was stored or printed, so a command that sends the wrong
// body or route fails here.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// escalationCLIDaemon stores the failure escalation override per "prd|task"
// (the raw policy JSON) and records each request as "METHOD path".
type escalationCLIDaemon struct {
	overrides  map[string]json.RawMessage
	resolved   map[string]any
	config     map[string]any
	hits       []string
	prdDetail  string
	configBody string
}

// startEscalationCLIDaemon points serverURL at a stand-in daemon for the test.
func startEscalationCLIDaemon(t *testing.T) *escalationCLIDaemon {
	t.Helper()
	d := &escalationCLIDaemon{
		overrides: map[string]json.RawMessage{},
		resolved:  map[string]any{},
		config:    map[string]any{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.hits = append(d.hits, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/autonomous/prds/p1":
			_, _ = w.Write([]byte(d.prdDetail))
			return
		case r.Method == http.MethodGet && r.URL.Path == "/api/config":
			_, _ = w.Write([]byte(d.configBody))
			return
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/set_failure_escalation"):
			prd := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds/"), "/set_failure_escalation")
			task, _ := body["task_id"].(string)
			policy, _ := json.Marshal(body["policy"])
			if string(policy) == "null" {
				delete(d.overrides, prd+"|"+task)
			} else {
				d.overrides[prd+"|"+task] = policy
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resolve_task_review"):
			d.resolved = body
		case r.Method == http.MethodPut && r.URL.Path == "/api/config":
			for k, v := range body {
				d.config[k] = v
			}
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return d
}

// runEscalationCmd runs c with args and returns what it printed to stdout.
func runEscalationCmd(t *testing.T, c *cobra.Command, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	c.SetArgs(args)
	c.SilenceUsage = true
	c.SilenceErrors = true
	runErr := c.Execute()
	os.Stdout = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

// TestFailureEscalationCLISetStoresTaskOverride: set --task stores the policy on
// that task only, with each flag it was given, and the steps text is read into
// the worker ladder (a - means the task's normal worker).
func TestFailureEscalationCLISetStoresTaskOverride(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	if _, err := runEscalationCmd(t, newAutonomousFailureEscalationSetCmd(),
		"p1", "--task", "t1", "--enabled", "false", "--max-failures", "4",
		"--steps", "pi-prompt:ollama/qwen3.8:27b@2,-"); err != nil {
		t.Fatalf("set: %v", err)
	}
	raw, ok := d.overrides["p1|t1"]
	if !ok {
		t.Fatalf("no override stored for p1/t1; hits %v", d.hits)
	}
	var got autonomouspkg.FailureEscalationPolicy
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	enabled := false
	want := autonomouspkg.FailureEscalationPolicy{
		Enabled:     &enabled,
		MaxFailures: 4,
		Steps: []autonomouspkg.FailureEscalationStep{
			{Backend: "pi-prompt", Model: "ollama/qwen3.8:27b", Attempts: 2},
			{},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored policy = %+v\nwant %+v", got, want)
	}
	if _, ok := d.overrides["p1|"]; ok {
		t.Error("the Automaton level must stay untouched when --task is given")
	}
}

// TestFailureEscalationCLIClearRemovesAutomatonOverride: clear sends a null
// policy, which removes the override stored at that level.
func TestFailureEscalationCLIClearRemovesAutomatonOverride(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	if _, err := runEscalationCmd(t, newAutonomousFailureEscalationSetCmd(), "p1", "--max-failures", "2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok := d.overrides["p1|"]; !ok {
		t.Fatal("set did not store the Automaton override")
	}
	if _, err := runEscalationCmd(t, newAutonomousFailureEscalationClearCmd(), "p1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := d.overrides["p1|"]; ok {
		t.Error("clear left the Automaton override in place")
	}
}

// TestFailureEscalationCLIEnabledFlagIsTrueOrFalse: --enabled takes true or
// false as a separate word, and anything else is refused before any request.
func TestFailureEscalationCLIEnabledFlagIsTrueOrFalse(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	if _, err := runEscalationCmd(t, newAutonomousFailureEscalationSetCmd(), "p1", "--enabled", "true"); err != nil {
		t.Fatalf("--enabled true: %v", err)
	}
	var got autonomouspkg.FailureEscalationPolicy
	if err := json.Unmarshal(d.overrides["p1|"], &got); err != nil || got.Enabled == nil || !*got.Enabled {
		t.Fatalf("stored enabled = %+v (err %v), want true", got.Enabled, err)
	}
	before := len(d.hits)
	if _, err := runEscalationCmd(t, newAutonomousFailureEscalationSetCmd(), "p1", "--enabled", "yes"); err == nil {
		t.Fatal("--enabled yes must be refused")
	}
	if len(d.hits) != before {
		t.Errorf("a refused --enabled still reached the daemon: %v", d.hits[before:])
	}
}

// TestFailureEscalationCLIShowPrintsOverrideAndDefault: show prints the stored
// Automaton and task overrides, the task's escalation counter and the daemon
// default, and names the rule that resolves them.
func TestFailureEscalationCLIShowPrintsOverrideAndDefault(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	d.prdDetail = `{"id":"p1","title":"First Automaton","failure_escalation":{"max_failures":2},
		"stories":[{"id":"s1","tasks":[{"id":"t1","title":"task one","status":"review_needed","escalation_failures":2,
		"failure_escalation":{"enabled":false}}]}]}`
	d.configBody = `{"autonomous":{"failure_escalation":{"enabled":true,"max_failures":3,"steps":[]}}}`
	out, err := runEscalationCmd(t, newAutonomousFailureEscalationShowCmd(), "p1", "--task", "t1")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var view struct {
		Automaton struct {
			ID                string          `json:"id"`
			FailureEscalation json.RawMessage `json:"failure_escalation"`
		} `json:"automaton"`
		Task struct {
			ID                 string          `json:"id"`
			EscalationFailures int             `json:"escalation_failures"`
			FailureEscalation  json.RawMessage `json:"failure_escalation"`
		} `json:"task"`
		DaemonDefault json.RawMessage `json:"daemon_default"`
		Rule          string          `json:"rule"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("show output is not JSON: %v\n%s", err, out)
	}
	if view.Automaton.ID != "p1" || !reflect.DeepEqual(jsonValue(t, view.Automaton.FailureEscalation), map[string]any{"max_failures": float64(2)}) {
		t.Errorf("automaton view = %+v", view.Automaton)
	}
	if view.Task.ID != "t1" || view.Task.EscalationFailures != 2 || !reflect.DeepEqual(jsonValue(t, view.Task.FailureEscalation), map[string]any{"enabled": false}) {
		t.Errorf("task view = %+v", view.Task)
	}
	if got := jsonValue(t, view.DaemonDefault); !reflect.DeepEqual(got.(map[string]any)["max_failures"], float64(3)) {
		t.Errorf("daemon default = %v", got)
	}
	if !strings.Contains(view.Rule, "task, then Automaton, then the daemon default") {
		t.Errorf("rule = %q", view.Rule)
	}
}

// jsonValue decodes raw JSON into a generic value, so a comparison ignores the
// indentation that show's output carries.
func jsonValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not JSON: %s: %v", raw, err)
	}
	return v
}

// TestFailureEscalationCLIShowRefusesUnknownTask: a --task that is not on the
// Automaton is an error, not an empty view.
func TestFailureEscalationCLIShowRefusesUnknownTask(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	d.prdDetail = `{"id":"p1","stories":[]}`
	d.configBody = `{"autonomous":{}}`
	if _, err := runEscalationCmd(t, newAutonomousFailureEscalationShowCmd(), "p1", "--task", "nope"); err == nil {
		t.Fatal("show --task for a task that is not on the Automaton must fail")
	}
}

// TestFailureEscalationCLIResolveReviewRetryBody: resolve-review retry sends the
// worker, the narrowed files and the note, and the actor is the operator.
func TestFailureEscalationCLIResolveReviewRetryBody(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	if _, err := runEscalationCmd(t, newAutonomousResolveReviewCmd(), "p1",
		"--task", "t1", "--action", "retry", "--backend", "claude-code", "--model", "claude-opus-5-5",
		"--effort", "high", "--files", "a.go, b.go", "--note", "narrow it"); err != nil {
		t.Fatalf("resolve-review: %v", err)
	}
	if d.resolved["task_id"] != "t1" || d.resolved["action"] != "retry" || d.resolved["backend"] != "claude-code" ||
		d.resolved["model"] != "claude-opus-5-5" || d.resolved["effort"] != "high" || d.resolved["note"] != "narrow it" ||
		d.resolved["actor"] != "operator" {
		t.Errorf("resolve body = %#v", d.resolved)
	}
	if !reflect.DeepEqual(d.resolved["files"], []any{"a.go", "b.go"}) {
		t.Errorf("files = %#v", d.resolved["files"])
	}
}

// TestFailureEscalationCLIConfigSetMaxFailures: datawatch config set sends the
// escalation key as a number, and the daemon's stored config carries it.
func TestFailureEscalationCLIConfigSetMaxFailures(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	cfg := &config.Config{}
	if _, err := captureEscalationStdout(t, func() error {
		return runConfigSet(cfg, "autonomous.failure_escalation.max_failures", "4")
	}); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if got := d.config["autonomous.failure_escalation.max_failures"]; got != float64(4) {
		t.Errorf("stored max_failures = %#v, want 4", got)
	}
}

// captureEscalationStdout runs fn and returns what it printed.
func captureEscalationStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

// TestFailureEscalationCLIHelpNamesEveryVerb: the autonomous help lists the
// escalation verbs, with show and resolve-review, and the status help names the
// review_needed array.
func TestFailureEscalationCLIHelpNamesEveryVerb(t *testing.T) {
	help := newAutonomousCmd().Long
	for _, want := range []string{"failure-escalation show|set|clear|global", "resolve-review"} {
		if !strings.Contains(help, want) {
			t.Errorf("autonomous help lacks %q", want)
		}
	}
	fe := newAutonomousFailureEscalationCmd().Long
	for _, want := range []string{"show <automaton-id>", "set <automaton-id>", "clear <automaton-id>", "--enabled true|false"} {
		if !strings.Contains(fe, want) {
			t.Errorf("failure-escalation help lacks %q", want)
		}
	}
	if st := newAutonomousStatusCmd().Long; !strings.Contains(st, "review_needed") {
		t.Errorf("status help lacks review_needed:\n%s", st)
	}
}
