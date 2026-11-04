// comm-channel `autonomous set-llm` / `set-story-llm` / `set-task-llm` —
// the chat reply carries the queued-requeue structured outcome: changed
// tasks (capacity_moved named), left-alone tasks with the reason, and the
// planning requeue (pools old → new, or the deferred explanation) — as a
// short chat-friendly line, the same result the REST/MCP/CLI surfaces
// show.

package router

import (
	"net/http"
	"strings"
	"testing"
)

var outcomeEnvelope = `{"ok":true,"override":{"changed":[{"task_id":"t1","capacity_moved":true},{"task_id":"t2"}],
"left_alone":[{"task_id":"t3","reason":"task is running; the override takes no effect while it runs"}]},
"planning":{"outcome":"requeued","old_pools":["claude-code"],"new_pools":["opencode"],
"reason":"the queued planning run was retargeted from claude-code to opencode"}}`

var outcomeWants = []string{
	"changed: t1 (capacity retargeted), t2",
	"left alone: t3 \u2014 task is running; the override takes no effect while it runs",
	"planning requeued (pools claude-code \u2192 opencode)",
	"\u2014 the queued planning run was retargeted from claude-code to opencode",
}

func TestCommSetLLMFamiliesSurfaceQueuedOutcome(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(outcomeEnvelope))
	})
	r := setupPeerRouter(t, port)

	msgs := map[string]string{
		"prd":   "autonomous set-llm p1 opencode",
		"story": "autonomous set-story-llm p1 s1 opencode",
		"task":  "autonomous set-task-llm p1 t1 opencode",
	}
	for name, msg := range msgs {
		resp := strings.Join(r.HandleTestMessage(msg), "\n")
		for _, want := range outcomeWants {
			if !strings.Contains(resp, want) {
				t.Errorf("%s (%s): reply missing %q:\n%s", name, msg, want, resp)
			}
		}
	}
}

func TestCommSetLLMPlanningDeferredExplains(t *testing.T) {
	env := `{"ok":true,"override":{"changed":[],
		"left_alone":[{"task_id":"t3","reason":"task is running; the override takes no effect while it runs"}]},
		"planning":{"outcome":"deferred_to_next_run","old_pools":["claude-code"],"new_pools":["opencode"],
			"reason":"decompose is already running (the planning run holds a live lease): wait for it to finish, or cancel the PRD to requeue on the current pool."}}`
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(env))
	})
	r := setupPeerRouter(t, port)

	resp := strings.Join(r.HandleTestMessage("autonomous set-llm p1 opencode"), "\n")
	for _, want := range []string{
		"left alone: t3 \u2014 task is running",
		"planning deferred_to_next_run (pools claude-code \u2192 opencode)",
		"wait for it to finish, or cancel the PRD",
	} {
		if !strings.Contains(resp, want) {
			t.Errorf("reply missing %q:\n%s", want, resp)
		}
	}
}

func TestCommSetLLMClassicReplyUnchanged(t *testing.T) {
	// A classic accepted reply (no override/planning fields) is relayed
	// as-is, with no outcome line invented.
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	r := setupPeerRouter(t, port)

	resp := strings.Join(r.HandleTestMessage("autonomous set-task-llm p1 t1 opencode"), "\n")
	if !strings.Contains(resp, `"ok": true`) {
		t.Errorf("classic reply not relayed: %s", resp)
	}
	if strings.Contains(resp, "outcome:") || strings.Contains(resp, "planning requeued") {
		t.Errorf("outcome invented for a classic reply: %s", resp)
	}
}
