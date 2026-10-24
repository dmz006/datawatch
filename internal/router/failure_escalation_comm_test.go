// Comm-channel verbs for failure escalation: set-failure-escalation and
// review-task. Each test runs the verb through the router and checks the request
// the loopback REST route receives, plus the reply text for the refusals.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// failureEscalationRecorder is a loopback REST stand-in. It keeps the last
// request's method, path and JSON body.
type failureEscalationRecorder struct {
	method, path string
	body         map[string]any
	hits         int
}

func newFailureEscalationRouter(t *testing.T) (*Router, *failureEscalationRecorder) {
	t.Helper()
	rec := &failureEscalationRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.hits++
		rec.method, rec.path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		rec.body = nil
		_ = json.Unmarshal(raw, &rec.body)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter("h", "g", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, rec
}

func TestFailureEscalationCommSetAutomatonOverride(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	out := r.HandleTestMessage("autonomous set-failure-escalation p1 max_failures=4 enabled=false")
	if len(out) == 0 || strings.Contains(strings.Join(out, "\n"), "failed") {
		t.Fatalf("reply = %v", out)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/prds/p1/set_failure_escalation" {
		t.Fatalf("request = %s %s", rec.method, rec.path)
	}
	if rec.body["task_id"] != "" {
		t.Errorf("task_id = %v, want empty for the Automaton level", rec.body["task_id"])
	}
	policy, _ := rec.body["policy"].(map[string]any)
	if policy["max_failures"] != float64(4) || policy["enabled"] != false {
		t.Errorf("policy = %v", policy)
	}
}

func TestFailureEscalationCommSetTaskStepsAndClear(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	r.HandleTestMessage("autonomous set-failure-escalation p1 t9 steps=pi-prompt:ollama/qwen3.8:27b@2,-")
	if rec.body["task_id"] != "t9" {
		t.Fatalf("task_id = %v, want t9", rec.body["task_id"])
	}
	steps, _ := rec.body["policy"].(map[string]any)["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("steps = %v, want 2", steps)
	}
	first := steps[0].(map[string]any)
	if first["backend"] != "pi-prompt" || first["model"] != "ollama/qwen3.8:27b" || first["attempts"] != float64(2) {
		t.Errorf("first step = %v", first)
	}

	r.HandleTestMessage("autonomous set-failure-escalation p1 t9 clear")
	if p, ok := rec.body["policy"]; !ok || p != nil {
		t.Fatalf("clear should send policy null, body = %v", rec.body)
	}
}

func TestFailureEscalationCommRefusesBadSettings(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	cases := map[string]string{
		"autonomous set-failure-escalation p1 max_failures=0": "max_failures must be",
		"autonomous set-failure-escalation p1 enabled=maybe":  "enabled must be true or false",
		"autonomous set-failure-escalation p1 steps=claude":   "needs the form backend:model",
		"autonomous set-failure-escalation p1 bogus=1":        "unknown key",
		"autonomous set-failure-escalation p1 max_failures":   "usage: autonomous set-failure-escalation",
		"autonomous set-failure-escalation":                   "usage: autonomous set-failure-escalation",
	}
	for text, want := range cases {
		out := strings.Join(r.HandleTestMessage(text), "\n")
		if !strings.Contains(out, want) {
			t.Errorf("%q: reply %q lacks %q", text, out, want)
		}
	}
	if rec.hits != 0 {
		t.Fatalf("a refused verb reached the REST loopback %d times", rec.hits)
	}
}

func TestFailureEscalationCommReviewTaskRetryWithNote(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	r.HandleTestMessage("autonomous review-task p1 t1 retry backend=claude-code model=claude-opus-5-5 files=a.go,b.go note=try the smaller change now")
	if rec.path != "/api/autonomous/prds/p1/resolve_task_review" {
		t.Fatalf("path = %s", rec.path)
	}
	if rec.body["action"] != "retry" || rec.body["task_id"] != "t1" || rec.body["backend"] != "claude-code" {
		t.Errorf("body = %v", rec.body)
	}
	if got := rec.body["note"]; got != "try the smaller change now" {
		t.Errorf("note = %q, want the words joined", got)
	}
	files, _ := rec.body["files"].([]any)
	if len(files) != 2 || files[1] != "b.go" {
		t.Errorf("files = %v", files)
	}
}

func TestFailureEscalationCommReviewTaskCancel(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	out := strings.Join(r.HandleTestMessage("autonomous review-task p1 t1 cancel"), "\n")
	if strings.Contains(out, "failed") {
		t.Fatalf("reply = %s", out)
	}
	if rec.body["action"] != "cancel" {
		t.Fatalf("body = %v", rec.body)
	}
}

func TestFailureEscalationCommReviewTaskRefusals(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	cases := map[string]string{
		"autonomous review-task p1 t1 explode":          "action must be retry or cancel",
		"autonomous review-task p1 t1 retry colour=red": "unknown key",
		"autonomous review-task p1 t1 retry stray":      "needs the form key=value",
		"autonomous review-task p1":                     "usage: autonomous review-task",
	}
	for text, want := range cases {
		out := strings.Join(r.HandleTestMessage(text), "\n")
		if !strings.Contains(out, want) {
			t.Errorf("%q: reply %q lacks %q", text, out, want)
		}
	}
	if rec.hits != 0 {
		t.Fatalf("a refused verb reached the REST loopback %d times", rec.hits)
	}
}
