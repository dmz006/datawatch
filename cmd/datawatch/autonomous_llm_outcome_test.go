// Task 6cb90009 — the CLI's explicit outcome line for the set-llm family:
// which targets the override landed on (changed) vs left alone (running /
// finished), and whether a planning requeue was requeued, deferred to the
// next run, or a no-op — printed before the daemon's pretty JSON.

package main

import (
	"testing"
)

func TestLLMOutcomeReply(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp string
		want string
	}{
		{
			name: "override: changed + left-alone with per-task statuses",
			resp: `{"id":"prda","override":{"changed":[{"task_id":"t1"},{"task_id":"t3"}],"left_alone":[{"task_id":"t2","reason":"task is starting up"}]}}`,
			want: "changed: t1, t3 | left alone: t2 (task is starting up)",
		},
		{
			name: "override: nothing changed (all started/finished)",
			resp: `{"override":{"changed":[],"left_alone":[{"task_id":"t1","reason":"task already started"}]}}`,
			want: "changed: (none) | left alone: t1 (task already started)",
		},
		{
			name: "planning: requeued between pools",
			resp: `{"planning":{"outcome":"requeued","old_pools":["fast"],"new_pools":["slow"]}}`,
			want: "requeued planner: fast -> slow",
		},
		{
			name: "planning: requeued with backend/model pair",
			resp: `{"planning":{"outcome":"requeued","old_backend":"opencode","old_model":"qwen3.8:27b","new_backend":"claude-code","new_model":"opus"}}`,
			want: "requeued planner: opencode/qwen3.8:27b -> claude-code/opus",
		},
		{
			name: "planning: deferred to next run",
			resp: `{"planning":{"outcome":"deferred_to_next_run","reason":"a planning run is already queued"}}`,
			want: "deferred to next run — a planning run is already queued",
		},
		{
			name: "planning: deferred with no reason",
			resp: `{"planning":{"outcome":"deferred_to_next_run"}}`,
			want: "deferred to next run (a planning run is already started)",
		},
		{
			name: "planning: unchanged (applies at next decompose)",
			resp: `{"planning":{"outcome":"unchanged","reason":"no planning run is queued or started; the stored value applies at the next decompose"}}`,
			want: "planner unchanged — no planning run is queued or started; the stored value applies at the next decompose",
		},
		{
			name: "both override and planning present",
			resp: `{"id":"prda","override":{"changed":[{"task_id":"t1"}],"left_alone":[]},"planning":{"outcome":"requeued","old_pools":["fast"],"new_pools":["slow"]}}`,
			want: "changed: t1 | left alone: (none) | requeued planner: fast -> slow",
		},
		{
			name: "classic accepted reply has no outcome",
			resp: `{"id":"prda","status":"running","version":9}`,
			want: "",
		},
		{
			name: "empty body",
			resp: ``,
			want: "",
		},
		{
			name: "non-JSON body",
			resp: `ok`,
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := llmOutcomeReply([]byte(tc.resp))
			if got != tc.want {
				t.Errorf("llmOutcomeReply = %q, want %q", got, tc.want)
			}
		})
	}
}
