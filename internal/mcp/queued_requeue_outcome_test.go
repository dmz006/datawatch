// Queued requeue — the set_llm MCP tools (autonomous_prd_set_llm,
// autonomous_prd_set_story_llm, autonomous_prd_set_task_llm) surface the
// structured outcome the daemon added for running-Automaton saves: the
// `override` result (changed tasks — each with capacity_moved when its
// queued capacity request was retargeted — and left-alone tasks with the
// reason) and the `planning` result (requeued | deferred_to_next_run |
// unchanged, old/new pools, reason). The tools pass the JSON straight
// through AND append a flat one-line `outcome: ...` summary rendered with
// the autonomous package's formatters, and their descriptions say so.

package mcp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const requeuedEnvelope = `{"ok":true,"override":{"changed":[{"task_id":"t1","capacity_moved":true},{"task_id":"t2"}],
"left_alone":[{"task_id":"t3","reason":"task is running; the override takes no effect while it runs"}]},
"planning":{"outcome":"requeued","old_backend":"claude-code","new_backend":"opencode",
"old_pools":["claude-code"],"new_pools":["opencode"],
"reason":"the queued planning run was retargeted from claude-code to opencode"}}`

const deferredEnvelope = `{"ok":true,"override":{"changed":[],
"left_alone":[{"task_id":"t3","reason":"task is running; the override takes no effect while it runs"}]},
"planning":{"outcome":"deferred_to_next_run","old_pools":["claude-code"],"new_pools":["opencode"],
"reason":"decompose is already running (the planning run holds a live lease): wait for it to finish, or cancel the PRD to requeue on the current pool."}}`

func outcomeText(t *testing.T, bind func(*Server) handlerFn, args map[string]any, reply string) string {
	t.Helper()
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(reply))
	})
	return invoke(t, bind(s), args)
}

func TestQueuedRequeue_OutcomeSummaryLine(t *testing.T) {
	b1 := func(s *Server) handlerFn { return s.handleAutonomousPRDSetLLM }
	b2 := func(s *Server) handlerFn { return s.handleAutonomousPRDSetStoryLLM }
	b3 := func(s *Server) handlerFn { return s.handleAutonomousPRDSetTaskLLM }

	text := outcomeText(t, b1, map[string]any{"id": "p1", "decomposition_profile": "opencode"}, requeuedEnvelope)
	// The structured JSON passes through verbatim, so a consumer that reads
	// the fields still sees them.
	for _, want := range []string{
		`"outcome":"requeued"`,
		`"capacity_moved":true`,
		`"left_alone"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("structured field missing %s: %s", want, text)
		}
	}
	// And the flat summary line renders the full result, in one line.
	for _, want := range []string{
		"outcome: changed: t1 (capacity retargeted), t2",
		"left alone: t3 \u2014 task is running; the override takes no effect while it runs",
		"planning requeued (pools claude-code \u2192 opencode)",
		"the queued planning run was retargeted from claude-code to opencode",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("outcome summary missing %q: %s", want, text)
		}
	}

	text = outcomeText(t, b2, map[string]any{"id": "p1", "story_id": "s1", "backend": "opencode"}, requeuedEnvelope)
	if !strings.Contains(text, "left alone: t3 \u2014 task is running") {
		t.Errorf("story set_llm lost the left-alone tasks: %s", text)
	}
	text = outcomeText(t, b3, map[string]any{"id": "p1", "task_id": "t9", "backend": "opencode"}, requeuedEnvelope)
	if !strings.Contains(text, "changed: t1 (capacity retargeted), t2") {
		t.Errorf("task set_llm lost the changed tasks: %s", text)
	}
}

func TestQueuedRequeue_PlanningDeferredExplains(t *testing.T) {
	b := func(s *Server) handlerFn { return s.handleAutonomousPRDSetLLM }
	text := outcomeText(t, b, map[string]any{"id": "p1", "decomposition_profile": "opencode"}, deferredEnvelope)
	for _, want := range []string{
		`"outcome":"deferred_to_next_run"`,
		"planning deferred_to_next_run (pools claude-code \u2192 opencode)",
		"wait for it to finish, or cancel the PRD",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("deferred explanation missing %q: %s", want, text)
		}
	}
}

func TestQueuedRequeue_ClassicPlainReplyUnchanged(t *testing.T) {
	// A classic-accepted (or refused) reply carries neither field: no
	// outcome line is appended and the body passes through untouched.
	b := func(s *Server) handlerFn { return s.handleAutonomousPRDSetTaskLLM }
	text := outcomeText(t, b, map[string]any{"id": "p1", "task_id": "t9", "backend": "opencode"},
		`{"ok":true,"message":"LLM config updated"}`)
	if !strings.Contains(text, `"message":"LLM config updated"`) {
		t.Fatalf("body not passed through: %s", text)
	}
	if strings.Contains(text, "outcome:") {
		t.Errorf("outcome line appended to a plain reply: %s", text)
	}
}

func TestQueuedRequeue_SetLLMToolDescriptions(t *testing.T) {
	s := &Server{}
	prdD := s.toolAutonomousPRDSetLLM().Description
	storyD := s.toolAutonomousPRDSetStoryLLM().Description
	taskD := s.toolAutonomousPRDSetTaskLLM().Description
	for name, d := range map[string]string{
		"autonomous_prd_set_llm":       prdD,
		"autonomous_prd_set_story_llm": storyD,
		"autonomous_prd_set_task_llm":  taskD,
	} {
		for _, want := range []string{"outcome:", "override"} {
			if !strings.Contains(d, want) {
				t.Errorf("%s description lacks %q", name, want)
			}
		}
	}
	if !strings.Contains(prdD, "planning") || !strings.Contains(prdD, "deferred_to_next_run") {
		t.Errorf("prd set_llm description lacks the planning outcome: %s", prdD)
	}
	for name, d := range map[string]string{
		"autonomous_prd_set_story_llm": storyD,
		"autonomous_prd_set_task_llm":  taskD,
	} {
		if !strings.Contains(d, "left-alone") && !strings.Contains(d, "left_alone") {
			t.Errorf("%s description does not name the left-alone tasks: %s", name, d)
		}
		if !strings.Contains(d, "capacity_moved") {
			t.Errorf("%s description does not mention capacity_moved: %s", name, d)
		}
	}
	cfgD := s.toolConfigSet().Description
	if !strings.Contains(cfgD, "autonomous.override_queued_tasks") {
		t.Errorf("config_set description lacks autonomous.override_queued_tasks: %s", cfgD)
	}
}

// Compile-time guard: the outcome rendering consumes the autonomous
// package's formatters, so the MCP surface cannot drift from the
// canonical wording.
var (
	_ = autonomous.FormatLLMOverrideResult
	_ = autonomous.FormatPlanningRequeueResult
)
