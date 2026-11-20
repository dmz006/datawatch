package router

import (
	"net/http"
	"strings"
	"testing"
)

// Comm-channel coverage for the Automation run-shape settings: the
// set-max-parallel and set-stage-gate verbs, insert with a decision wait, and
// the run-shape line of the status reply. Each request is checked against the
// REST call the verb makes; the parent is a fake daemon (see setLLMFixture).

const (
	maxParallelUsagePrefix = "usage: automation set-max-parallel"
	stageGateUsagePrefix   = "usage: automation set-stage-gate"
)

func TestAutomationCommSetMaxParallel(t *testing.T) {
	r, calls := setLLMFixture(t)
	path := "/api/automations/ab12/set_max_parallel"
	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"bare number", "automation set-max-parallel ab12 3", path,
			map[string]any{"actor": "comm", "max_parallel": float64(3)}},
		{"zero clears", "automation set-max-parallel ab12 0", path,
			map[string]any{"actor": "comm", "max_parallel": float64(0)}},
		{"keyed", "automation set-max-parallel ab12 max_parallel=2", path,
			map[string]any{"actor": "comm", "max_parallel": float64(2)}},
		{"hyphen key", "automation set-max-parallel ab12 max-parallel=4", path,
			map[string]any{"actor": "comm", "max_parallel": float64(4)}},
		{"fork", "automation set-max-parallel ab12 2 member=split", path,
			map[string]any{"actor": "comm", "max_parallel": float64(2), "member_id": "split"}},
		{"member first and version", "automation set-max-parallel ab12 member_id=split 1 expected_version=6", path,
			map[string]any{"actor": "comm", "max_parallel": float64(1), "member_id": "split", "expected_version": float64(6)}},
	})
}

func TestAutomationCommSetMaxParallelUsage(t *testing.T) {
	r, calls := setLLMFixture(t)
	runSetLLMUsage(t, r, calls, []string{
		"automation set-max-parallel",
		"automation set-max-parallel ab12",
		"automation set-max-parallel ab12 many",
		"automation set-max-parallel ab12 -1",
		"automation set-max-parallel ab12 1.5",
		"automation set-max-parallel ab12 2 3",
		"automation set-max-parallel ab12 member=split",
		"automation set-max-parallel ab12 2 bogus=1",
		"automation set-max-parallel ab12 max_parallel=2 3",
	}, maxParallelUsagePrefix)

	// A version that is not a number names the problem and the usage.
	calls.reset()
	resp := r.HandleTestMessage("automation set-max-parallel ab12 2 expected_version=x")
	if len(calls.snapshot()) != 0 {
		t.Error("a bad expected_version reached the daemon")
	}
	if len(resp) == 0 || !strings.Contains(resp[0], "expected_version must be a whole number") || !strings.Contains(resp[0], maxParallelUsagePrefix) {
		t.Errorf("reply = %v", resp)
	}
}

func TestAutomationCommSetStageGate(t *testing.T) {
	r, calls := setLLMFixture(t)
	path := "/api/automations/ab12/set_stage_gate"
	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"on", "automation set-stage-gate ab12 on", path, map[string]any{"actor": "comm", "stage_gate": true}},
		{"off", "automation set-stage-gate ab12 off", path, map[string]any{"actor": "comm", "stage_gate": false}},
		{"true", "automation set-stage-gate ab12 true", path, map[string]any{"actor": "comm", "stage_gate": true}},
		{"false upper", "automation set-stage-gate ab12 FALSE", path, map[string]any{"actor": "comm", "stage_gate": false}},
		{"keyed with version", "automation set-stage-gate ab12 stage_gate=on expected_version=3", path,
			map[string]any{"actor": "comm", "stage_gate": true, "expected_version": float64(3)}},
	})
}

func TestAutomationCommSetStageGateUsage(t *testing.T) {
	r, calls := setLLMFixture(t)
	runSetLLMUsage(t, r, calls, []string{
		"automation set-stage-gate",
		"automation set-stage-gate ab12",
		"automation set-stage-gate ab12 maybe",
		"automation set-stage-gate ab12 on off",
		"automation set-stage-gate ab12 on member=m1",
		"automation set-stage-gate ab12 on bogus=1",
	}, stageGateUsagePrefix)
}

func TestAutomationCommConfirmationNamesTheChange(t *testing.T) {
	r, _ := setLLMFixture(t)
	for in, want := range map[string]string{
		"automation set-max-parallel ab12 3":              "max parallel set to 3",
		"automation set-max-parallel ab12 0":              "max parallel cleared (no limit)",
		"automation set-max-parallel ab12 2 member=split": "fork split max parallel set to 2",
		"automation set-stage-gate ab12 on":               "stage gate turned on",
		"automation set-stage-gate ab12 off":              "stage gate turned off",
	} {
		resp := r.HandleTestMessage(in)
		if len(resp) == 0 || !strings.Contains(resp[0], want) {
			t.Errorf("%q reply = %v, want it to contain %q", in, resp, want)
		}
	}
}

func TestAutomationCommInsertDecisionWait(t *testing.T) {
	r, calls := setLLMFixture(t)
	path := "/api/automations/ab12/insert_member"
	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"question and reason", "automation insert ab12 decision-wait Ship it now? | sign-off before release", path,
			map[string]any{"actor": "comm", "urgent": false, "reason": "sign-off before release",
				"member": map[string]any{"kind": "decision_wait", "question": "Ship it now?"}}},
		{"underscore kind with options", "automation insert ab12 decision_wait urgent stage=release Continue with the plan? | check in", path,
			map[string]any{"actor": "comm", "urgent": true, "reason": "check in",
				"member": map[string]any{"kind": "decision_wait", "question": "Continue with the plan?", "stage": "release"}}},
	})

	// The Automaton and pending item forms are unchanged.
	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"automaton", "automation insert ab12 c9d0e1f2 tests found a gap", path,
			map[string]any{"actor": "comm", "urgent": false, "reason": "tests found a gap", "member": map[string]any{"prd_id": "c9d0e1f2"}}},
	})

	// A question and a reason are both required, split by |.
	for _, in := range []string{
		"automation insert ab12 decision-wait",
		"automation insert ab12 decision-wait only a question",
		"automation insert ab12 decision-wait | only a reason",
		"automation insert ab12 decision-wait Ship it? |",
		"automation insert ab12 decision-wait urgent",
	} {
		calls.reset()
		resp := r.HandleTestMessage(in)
		if len(calls.snapshot()) != 0 {
			t.Errorf("%q reached the daemon", in)
		}
		if len(resp) == 0 || !strings.HasPrefix(resp[0], "usage: automation insert") {
			t.Errorf("%q reply = %v, want the insert usage", in, resp)
		}
	}
}

func TestAutomationCommRunShapeVerbsListedAndRouted(t *testing.T) {
	listed := map[string]bool{}
	for _, v := range AutomationCommVerbs() {
		listed[v] = true
	}
	for _, v := range []string{"set-max-parallel", "set-stage-gate"} {
		if !listed[v] {
			t.Errorf("AutomationCommVerbs lacks %s", v)
		}
		if !strings.Contains(automationUsage, v) {
			t.Errorf("the automation usage lacks %s", v)
		}
	}
	if !strings.Contains(automationUsage, "decision-wait") {
		t.Error("the automation usage lacks the decision-wait insert")
	}
	r, _ := setLLMFixture(t)
	resp := r.HandleTestMessage("automation nope")
	if len(resp) == 0 || !strings.Contains(resp[0], "set-max-parallel") || !strings.Contains(resp[0], "set-stage-gate") {
		t.Errorf("unknown-subcommand reply = %v, want it to list the new verbs", resp)
	}
}

func TestAutomationCommStatusRunShapeLine(t *testing.T) {
	const shaped = `{"id":"ab12","name":"shaped","status":"active","version":4,"max_parallel":3,"stage_gate":true,"members":[` +
		`{"id":"f","kind":"fork","max_parallel":2},{"id":"b1","status":"running"},` +
		`{"id":"b2","waiting_for_slot":true},{"id":"b3","waiting_for_slot":true},{"id":"b4","waiting_for_stage":"build"},` +
		`{"id":"ask","kind":"decision_wait","waiting_for_operator":true}]}`
	const plain = `{"id":"cd34","name":"plain","status":"active","version":1,"members":[{"id":"m1"}]}`
	reply := shaped
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/automations/") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(reply))
	})
	r := setupRetryRouter(t, port)

	resp := r.HandleTestMessage("automation status ab12")
	if len(resp) == 0 {
		t.Fatal("no reply")
	}
	for _, want := range []string{
		"automation ab12 (shaped): status active, version 4, 6 members",
		"run shape: max 3 at once; fork f max 2; stage gate on; waiting: 2 for a free slot, 1 for an earlier stage, 1 for you",
	} {
		if !strings.Contains(resp[0], want) {
			t.Errorf("status reply lacks %q:\n%s", want, resp[0])
		}
	}

	// A chain with no limit, gate or waiting member has no run-shape line.
	reply = plain
	resp = r.HandleTestMessage("automation status cd34")
	if len(resp) == 0 || strings.Contains(resp[0], "run shape") {
		t.Errorf("a plain chain prints a run-shape line: %v", resp)
	}
}
