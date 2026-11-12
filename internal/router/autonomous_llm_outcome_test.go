// Task 6cb90009 — chat-friendly outcome lines for the PRD-level autonomous
// comm verbs: `set-llm` / `set-story-llm` / `set-task-llm` and
// `decompose`/`re-plan` replies now lead with an explicit line — which tasks
// the override landed on vs left alone (running/finished) and whether a
// planning requeue requeued / deferred / left the planner unchanged — before
// the daemon's pretty-printed JSON.

package router

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func autonomousOutcomeFixture(t *testing.T, resp string) *Router {
	t.Helper()
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(resp))
	})
	return setupPeerRouter(t, port)
}

func TestAutonomousCommSetLLM_OutcomeReplyLine(t *testing.T) {
	r := autonomousOutcomeFixture(t, `{"id":"prda","status":"running","version":9,"override":{
		"changed":[{"task_id":"t1"},{"task_id":"t3"}],
		"left_alone":[{"task_id":"t2","reason":"task is starting up"}]}}`)
	resp := r.HandleTestMessage("autonomous set-llm p1 model=x")
	if len(resp) == 0 {
		t.Fatal("no reply")
	}
	got := resp[0]
	for _, want := range []string{
		"autonomous set-llm",
		"updated: t1, t3",
		"left alone: t2 (task is starting up)",
		`"override"`, // the daemon's JSON still follows the line
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply missing %q:\n%s", want, got)
		}
	}
}

func TestAutonomousCommSetTaskLLM_OutcomeReplyLine(t *testing.T) {
	r := autonomousOutcomeFixture(t, `{"id":"prda","override":{"changed":[],
		"left_alone":[{"task_id":"t2","reason":"task is starting up"}]}}`)
	resp := r.HandleTestMessage("autonomous set-task-llm p1 --task t2 model=x")
	if len(resp) == 0 {
		t.Fatal("no reply")
	}
	if !strings.Contains(resp[0], "left alone: t2 (task is starting up)") {
		t.Errorf("reply missing left-alone line:\n%s", resp[0])
	}
}

func TestAutonomousCommDecompose_PlanningOutcomeReplyLine(t *testing.T) {
	r := autonomousOutcomeFixture(t, `{"id":"prda","planning":{"outcome":"requeued",
		"old_backend":"opencode","old_model":"small","new_backend":"claude-code","new_model":"big"}}`)
	resp := r.HandleTestMessage("autonomous decompose p1")
	if len(resp) == 0 {
		t.Fatal("no reply")
	}
	for _, want := range []string{"autonomous decompose", "planning requeued opencode/small", "claude-code/big"} {
		if !strings.Contains(resp[0], want) {
			t.Errorf("reply missing %q:\n%s", want, resp[0])
		}
	}
}

func TestAutonomousCommDecompose_PlanningDeferredReplyLine(t *testing.T) {
	r := autonomousOutcomeFixture(t, `{"id":"prda","planning":{"outcome":"deferred_to_next_run"}}`)
	resp := r.HandleTestMessage("autonomous decompose p1")
	if len(resp) == 0 {
		t.Fatal("no reply")
	}
	if !strings.Contains(resp[0], "planning deferred to next run") {
		t.Errorf("reply missing deferred line:\n%s", resp[0])
	}
}

func TestAutonomousCommSetLLM_PlainReplyUnchangedShape(t *testing.T) {
	// A classic accepted call (no structured outcome) keeps exactly the
	// reply shape it had before: title + pretty JSON, no outcome line.
	r := autonomousOutcomeFixture(t, `{"id":"prda","status":"running","version":9}`)
	resp := r.HandleTestMessage("autonomous set-llm p1 model=x")
	if len(resp) == 0 {
		t.Fatal("no reply")
	}
	got := resp[0]
	for _, banned := range []string{"updated:", "left alone:", "planning requeued", "planning deferred to next run"} {
		if strings.Contains(got, banned) {
			t.Errorf("plain reply unexpectedly contains %q:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, `"id":"prda"`) && !strings.Contains(got, "\"id\": \"prda\"") {
		t.Errorf("plain reply lost its JSON:\n%s", got)
	}
}

func TestCommLLMOutcomeLine_Unit(t *testing.T) {
	got := commLLMOutcomeLine(`{"override":{"changed":[{"task_id":"t1"}],"left_alone":[]},"planning":{"outcome":"requeued","old_backend":"a","old_model":"b","new_backend":"c","new_model":"d"}}`)
	for _, want := range []string{"updated: t1", "left alone: (none)", "planning requeued a/b", "c/d"} {
		if !strings.Contains(got, want) {
			t.Errorf("combined line missing %q:\n%s", want, got)
		}
	}
	if got := commLLMOutcomeLine(`{"id":"prda"}`); got != "" {
		t.Errorf("plain payload should yield no line, got %q", got)
	}
	if got := commLLMOutcomeLine("not even json"); got != "" {
		t.Errorf("non-JSON should yield no line, got %q", got)
	}
}
