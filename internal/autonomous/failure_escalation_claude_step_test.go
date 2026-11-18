package autonomous

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// claudeModelLessLadder is the failure escalation ladder these tests run: the
// first attempt keeps the fixture's normally resolved worker (opencode, no
// model), and every attempt after the first lands on a claude-code step that
// names no model of its own, so the executor must resolve the model from the
// package's explicit-model policy instead of launching on no model.
func claudeModelLessLadder() *FailureEscalationPolicy {
	return &FailureEscalationPolicy{
		Enabled:     escalationOn(true),
		MaxFailures: 3,
		Steps: []FailureEscalationStep{
			{Backend: "opencode", Attempts: 1},
			{Backend: escClaudeBackend},
		},
	}
}

// failTwiceThenPass returns a verifier that fails the first two attempts with
// distinct summaries (an identical triple would park the task in no-retry
// hold) and passes the third, so the Automaton completes with two recorded
// attempts.
func failTwiceThenPass() VerifyFn {
	var calls int
	return func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		calls++
		if calls <= 2 {
			return VerificationResult{OK: false, Summary: fmt.Sprintf("tests still fail (attempt %d)", calls)}, nil
		}
		return VerificationResult{OK: true}, nil
	}
}

// failureEscalationDecisionNotes returns a stored PRD's failure_escalation
// decision notes, in order.
func failureEscalationDecisionNotes(t *testing.T, m *Manager, prdID string) []string {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s not found", prdID)
	}
	var notes []string
	for _, d := range prd.Decisions {
		if d.Kind == "failure_escalation" {
			notes = append(notes, d.Note)
		}
	}
	return notes
}

// TestFailureEscalation_ClaudeStepWithoutModelResolvesSonnet: a ladder step
// whose backend is claude-code and which names no model runs a regular-kind
// task on sonnet — the same pairing resolveTaskExecutionLLM makes for a
// normally resolved worker — so the attempt never launches empty, and the
// spawn, the attempt record and the decision log all call it by the resolved
// model.
func TestFailureEscalation_ClaudeStepWithoutModelResolvesSonnet(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = claudeModelLessLadder()
	})

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, failTwiceThenPass())

	want := []string{"opencode/", escClaudeBackend + "/sonnet", escClaudeBackend + "/sonnet"}
	if workers := spawns.workers(); !reflect.DeepEqual(workers, want) {
		t.Fatalf("workers = %v, want %v", workers, want)
	}
	for i, req := range spawns.reqs {
		if IsClaudeBackend(req.Backend) && strings.TrimSpace(req.Model) == "" {
			t.Fatalf("spawn %d launched the claude backend %q with no model", i+1, req.Backend)
		}
	}

	// The fixture task ("Task 1" / "do task 1") is a regular kind, not a
	// mechanical one, so the resolved model is sonnet. Attempt 2 is the
	// failed claude attempt; attempt 3 passed and completes the task.
	task := escalationTask(t, m, prd.ID)
	if len(task.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(task.Attempts))
	}
	a := task.Attempts[1]
	if a.Backend != escClaudeBackend || a.Model != "sonnet" {
		t.Fatalf("attempt 2 worker = %s/%s, want %s/sonnet", a.Backend, a.Model, escClaudeBackend)
	}
	if !a.Escalated {
		t.Fatalf("attempt 2 marked not escalated, want the ladder step 2 adoption")
	}
	if !strings.Contains(a.BackendReason, "escalated after") {
		t.Fatalf("attempt 2 backend reason %q, want the escalation reason", a.BackendReason)
	}

	// The decision entry that logged the worker change names the step, the
	// backend and the model (which for this step the executor resolved).
	wantNote := []string{"step 2", escClaudeBackend, "sonnet"}
	found := false
	for _, note := range failureEscalationDecisionNotes(t, m, prd.ID) {
		if allContained(note, wantNote) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no decision entry with %v; notes: %v", wantNote, failureEscalationDecisionNotes(t, m, prd.ID))
	}
}

// TestFailureEscalation_ClaudeStepWithoutModelResolvesHaikuMechanical: the
// same model-less claude-code step on a mechanical-kind task resolves to
// haiku, the lighter model the policy picks for mechanical types.
func TestFailureEscalation_ClaudeStepWithoutModelResolvesHaikuMechanical(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.Story[0].Tasks[0].Title = "Add unit tests for the parser"
		p.FailureEscalation = claudeModelLessLadder()
	})

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, failTwiceThenPass())

	want := []string{"opencode/", escClaudeBackend + "/haiku", escClaudeBackend + "/haiku"}
	if workers := spawns.workers(); !reflect.DeepEqual(workers, want) {
		t.Fatalf("workers = %v, want %v", workers, want)
	}

	task := escalationTask(t, m, prd.ID)
	if len(task.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(task.Attempts))
	}
	if a := task.Attempts[1]; a.Backend != escClaudeBackend || a.Model != "haiku" {
		t.Fatalf("attempt 2 worker = %s/%s, want %s/haiku for the mechanical kind", a.Backend, a.Model, escClaudeBackend)
	}
}

// TestFailureEscalation_ClaudeStepNamedModelPassesThrough: a model a Claude
// step does name is never touched by the policy — only an empty one is
// resolved — and likewise a step model on a non-Claude backend is left alone.
func TestFailureEscalation_ClaudeStepNamedModelPassesThrough(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			Enabled:     escalationOn(true),
			MaxFailures: 3,
			Steps: []FailureEscalationStep{
				{Backend: "opencode", Model: "local-fast", Attempts: 1},
				{Backend: escClaudeBackend, Model: "claude-opus-5-5"},
			},
		}
	})

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, failTwiceThenPass())

	if workers := spawns.workers(); !reflect.DeepEqual(workers, []string{"opencode/local-fast", escClaudeBackend + "/claude-opus-5-5", escClaudeBackend + "/claude-opus-5-5"}) {
		t.Fatalf("workers = %v, want both named step models passed through", workers)
	}
}

// TestFailureEscalation_StepOnBackedOffBackendIsSkipped: while the usage
// guard has the step's backend backed off, the escalation never launches onto
// it: every attempt runs on the task's normally resolved worker, each records
// a decision entry naming the step, the backend and the skip for the attempt
// because the backend is backed off, and the failed attempt's record carries
// the same fact in its backend reason.
func TestFailureEscalation_StepOnBackedOffBackendIsSkipped(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	bo := NewBackoff()
	bo.Set(escClaudeBackend, time.Now().Add(time.Hour), "usage limit", BackoffSourceOperator)
	m.SetBackoff(bo)

	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			Enabled:     escalationOn(true),
			MaxFailures: 3,
			Steps:       []FailureEscalationStep{{Backend: escClaudeBackend, Model: "claude-opus-5-5"}},
		}
	})

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, failTwiceThenPass())

	// All three attempts (two failed, then the pass) ran on the normally
	// resolved worker — each recorded a skipped step — and none launched the
	// backed-off backend.
	if workers := spawns.workers(); !reflect.DeepEqual(workers, []string{escNormalWorker, escNormalWorker, escNormalWorker}) {
		t.Fatalf("workers = %v, want %v (the backed-off step is skipped for every attempt)", workers, []string{escNormalWorker, escNormalWorker, escNormalWorker})
	}
	for _, req := range spawns.reqs {
		if req.Backend == escClaudeBackend {
			t.Fatalf("spawn ran the backed-off backend %q (worker %q/%q)", escClaudeBackend, req.Backend, req.Model)
		}
	}

	// The failed attempts' records carry the skip in their backend reason.
	task := escalationTask(t, m, prd.ID)
	if len(task.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2 (fail, fail, pass)", len(task.Attempts))
	}
	for i := range task.Attempts {
		a := task.Attempts[i]
		if a.Backend != "opencode" || a.Escalated {
			t.Fatalf("attempt %d (backend %q escalated %v), want the normally resolved worker with no escalation", i+1, a.Backend, a.Escalated)
		}
		wantFacts := []string{"escalation step 1", escClaudeBackend, "skipped for this attempt", "backed off"}
		for _, want := range wantFacts {
			if !strings.Contains(a.BackendReason, want) {
				t.Fatalf("attempt %d backend reason %q missing %q", i+1, a.BackendReason, want)
			}
		}
	}

	// Every attempt recorded a skip naming the step, the backend and the
	// reason. The fact list above was defined on the attempt records; reuse
	// the same phrases for the decision entries.
	notes := failureEscalationDecisionNotes(t, m, prd.ID)
	if len(notes) != 3 {
		t.Fatalf("decision notes = %v, want one skip record per attempt (3)", notes)
	}
	for i, note := range notes {
		for _, want := range []string{"escalation step 1", escClaudeBackend, "skipped for this attempt", "backed off"} {
			if !strings.Contains(note, want) {
				t.Fatalf("decision note %d %q missing %q (all: %v)", i+1, note, want, notes)
			}
		}
	}
}

// allContained reports whether every want substring occurs in s.
func allContained(s string, want []string) bool {
	for _, w := range want {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}
