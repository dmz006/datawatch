package autonomous

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pause"
)

// The shipped-work refusal, the operator pause and the attempt cap each gate a
// start. These tests pin the order where two of them apply to the same
// Automaton: held, then closed or shipped work, then the pause, then capacity
// and the attempt cap. Shipped work is delivered work, so it never reports a
// pause wait and never reaches the attempt cap.

// A paused daemon plus a shipped queued Automaton: the refusal reason is
// already_shipped, not a pause wait. The Automaton is marked delivered and
// nothing spawns, through every door (Run, API.Run and the lane fill rule).
func TestShippedRefusalWinsOverOperatorPause(t *testing.T) {
	m, _ := newHoldManager(t)
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeGlobal, "", "release freeze", "operator"); err != nil {
		t.Fatal(err)
	}
	var spawned atomic.Int32
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return fakeSpawn(ctx, req)
	}

	t.Run("Run", func(t *testing.T) {
		prd := newShippedPRD(t, m, "Land the thing onto main", PRDApproved, shippedRelease, shippedCommit)
		err := m.Run(context.Background(), prd.ID, spawn, fakeVerify)
		if err == nil || !strings.Contains(err.Error(), "already shipped") || !strings.Contains(err.Error(), shippedRelease) {
			t.Fatalf("Run err = %v, want an already shipped refusal naming %s", err, shippedRelease)
		}
		if strings.Contains(err.Error(), "paused") || strings.Contains(err.Error(), "release freeze") {
			t.Fatalf("Run err = %q, a shipped Automaton must not report the pause", err)
		}
		got, _ := m.Store().GetPRD(prd.ID)
		if got.Status != PRDCompleted || got.ShippedRefusal == nil {
			t.Fatalf("status %s shipped_refusal %v, want delivered (completed) with the record", got.Status, got.ShippedRefusal)
		}
	})

	t.Run("APIRun", func(t *testing.T) {
		prd := newShippedPRD(t, m, "Land the other thing", PRDApproved, shippedRelease, shippedCommit)
		err := NewAPI(m).Run(prd.ID)
		if err == nil || !strings.Contains(err.Error(), "already shipped") || strings.Contains(err.Error(), "paused") {
			t.Fatalf("API.Run err = %v, want already shipped and no pause text", err)
		}
		got, _ := m.Store().GetPRD(prd.ID)
		if got.Status != PRDCompleted {
			t.Fatalf("status after API.Run = %s, want completed (delivered)", got.Status)
		}
	})

	t.Run("PausedUnshippedStillWaits", func(t *testing.T) {
		prd := newShippedPRD(t, m, "Not shipped", PRDApproved, "", "")
		err := m.Run(context.Background(), prd.ID, spawn, fakeVerify)
		if err == nil || !strings.Contains(err.Error(), "paused by operator") {
			t.Fatalf("Run err = %v, an unshipped Automaton keeps the pause refusal", err)
		}
		got, _ := m.Store().GetPRD(prd.ID)
		if got.Status != PRDApproved {
			t.Fatalf("status = %s, a pause refusal leaves the Automaton queued", got.Status)
		}
	})

	if n := spawned.Load(); n != 0 {
		t.Fatalf("spawned %d workers on a paused daemon", n)
	}

	t.Run("EligibilityRuleOrder", func(t *testing.T) {
		c := LaneFillCandidate{
			ID: "p1", Status: PRDApproved,
			PausedReason:   "paused by operator (all work): freeze",
			ShippedRelease: shippedRelease, ShippedCommit: shippedCommit,
		}
		code, text := EvaluateLaneFillEligibility(LaneFillConfig{}, c, time.Now())
		if code != LaneFillReasonAlreadyShipped || !strings.Contains(text, shippedRelease) {
			t.Fatalf("refusal = %q %q, want already_shipped before operator_pause", code, text)
		}
		c.ShippedRelease, c.ShippedCommit = "", ""
		if code, _ := EvaluateLaneFillEligibility(LaneFillConfig{}, c, time.Now()); code != LaneFillReasonOperatorPause {
			t.Fatalf("refusal = %q, want operator_pause once the shipped record is gone", code)
		}
		c.Status = PRDHeld
		c.ShippedRelease, c.ShippedCommit = shippedRelease, shippedCommit
		if code, _ := EvaluateLaneFillEligibility(LaneFillConfig{}, c, time.Now()); code != LaneFillReasonOperatorHold {
			t.Fatalf("refusal = %q, want operator_hold before already_shipped", code)
		}
	})
}

// Lane fill on a paused daemon drops shipped queued work as delivered and
// reports already_shipped for it, while an unshipped Automaton is reported as
// operator_pause.
func TestLaneFillReportsShippedNotPauseForShippedAutomaton(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	ps := newPauseStore(t)
	h.m.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeGlobal, "", "freeze", "operator"); err != nil {
		t.Fatal(err)
	}
	h.c.SetReady()
	shipped := h.addPRD("approved and shipped", PRDApproved, "claude", "claude-code")
	plain := h.addPRD("not shipped", PRDApproved, "claude", "claude-code")
	h.m.Store().MutatePRD(shipped.ID, func(p *PRD) bool {
		p.MergedInRelease, p.MergeCommit = shippedRelease, shippedCommit
		return true
	})

	h.poll()

	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("lane fill called %v on a paused daemon", calls)
	}
	st := h.status()
	d, ok := holdStartDecisionFor(st, shipped.ID)
	if !ok || d.EligibilityReason != LaneFillReasonAlreadyShipped {
		t.Fatalf("shipped decision = %+v (found %v), want already_shipped", d, ok)
	}
	d, ok = holdStartDecisionFor(st, plain.ID)
	if !ok || d.EligibilityReason != LaneFillReasonOperatorPause {
		t.Fatalf("unshipped decision = %+v (found %v), want operator_pause", d, ok)
	}
	got, _ := h.m.Store().GetPRD(shipped.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("shipped Automaton status = %s, want completed (delivered)", got.Status)
	}
	if got, _ := h.m.Store().GetPRD(plain.ID); got.Status != PRDApproved {
		t.Fatalf("unshipped Automaton status = %s, a pause leaves it queued", got.Status)
	}
}

// An Automaton with a task at the attempt cap that is also shipped: shipped
// wins. The start is refused before any attempt gate runs, the Automaton is
// dropped as delivered, and the cap raises no hold, question or alert.
func TestShippedRefusalWinsOverAttemptCap(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(2))
	r.addPRD(t)
	r.m.Store().MutatePRD(r.prd.ID, func(p *PRD) bool {
		p.MergedInRelease, p.MergeCommit = shippedRelease, shippedCommit
		return true
	})
	if !r.m.Store().MutatePRD(r.prd.ID, func(p *PRD) bool {
		for si := range p.Story {
			for ti := range p.Story[si].Tasks {
				p.Story[si].Tasks[ti].AttemptsTotal = 2
			}
		}
		return true
	}) {
		t.Fatal("Automaton not found")
	}
	if !r.m.attemptCapReached(r.prd, r.task(t)) {
		t.Fatal("setup: the task must be at the attempt cap")
	}

	r.api.SetExecutors(r.spawns.spawn, alwaysFailVerify)
	err := r.api.Run(r.prd.ID)
	if err == nil || !strings.Contains(err.Error(), "already shipped") {
		t.Fatalf("Run err = %v, want an already shipped refusal", err)
	}
	if strings.Contains(err.Error(), "attempt") {
		t.Fatalf("Run err = %q, the cap must not be named for shipped work", err)
	}
	got, _ := r.m.Store().GetPRD(r.prd.ID)
	if got.Status != PRDCompleted || got.ShippedRefusal == nil {
		t.Fatalf("status %s shipped_refusal %v, want delivered (completed) with the record", got.Status, got.ShippedRefusal)
	}
	task := r.task(t)
	if task.AttemptCapHeld || task.Status == TaskReviewNeeded {
		t.Fatalf("task held at the cap (status %s, held %v): the cap gate must not run for shipped work", task.Status, task.AttemptCapHeld)
	}
	if got := shippedDecisions(got, attemptCapHoldDecisionKind); len(got) != 0 {
		t.Fatalf("attempt cap hold decisions %v on shipped work", got)
	}
	if n := len(r.spawns.workers()); n != 0 {
		t.Fatalf("spawned %d workers for shipped work", n)
	}

	// Direct Run takes the same path.
	err = r.m.Run(context.Background(), r.prd.ID, r.spawns.spawn, alwaysFailVerify)
	if err == nil {
		t.Fatal("a second start of delivered work must be refused")
	}
}
