package autonomous

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pause"
)

// planGateFixture is a draft Automaton and a decompose function that counts its
// calls and returns a valid plan.
func planGateFixture(t *testing.T) (*Manager, *API, *PRD, *atomic.Int32) {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) {
		calls.Add(1)
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	return m, api, prd, &calls
}

func TestPlanningGate_AutomaticPlanningRefusedWhilePaused(t *testing.T) {
	m, _, prd, calls := planGateFixture(t)
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeGlobal, "", "freeze", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DecomposeAutomatic(prd.ID); err == nil || !strings.Contains(err.Error(), "paused by operator") {
		t.Fatalf("DecomposeAutomatic err = %v, want a pause refusal", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("a paused Automaton started %d planner call(s)", calls.Load())
	}
	if cur, _ := m.Store().GetPRD(prd.ID); cur.Status != PRDDraft {
		t.Fatalf("a refused start must leave the draft alone, status %q", cur.Status)
	}
	// The operator's own decompose is still allowed while paused.
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("an operator decompose must stay allowed while paused: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("operator decompose planner calls = %d, want 1", calls.Load())
	}
}

func TestPlanningGate_AcceptedPlanIsNotReplannedAutomatically(t *testing.T) {
	m, _, prd, calls := planGateFixture(t)
	if _, err := m.DecomposeAutomatic(prd.ID); err != nil {
		t.Fatal(err)
	}
	if cur, _ := m.Store().GetPRD(prd.ID); cur.Status != PRDNeedsReview {
		t.Fatalf("status after the plan = %q, want needs_review", cur.Status)
	}
	if _, err := m.DecomposeAutomatic(prd.ID); err == nil || !strings.Contains(err.Error(), "already has a plan") {
		t.Fatalf("a second automatic plan must be refused, err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("planner calls = %d, want 1", calls.Load())
	}
}

func TestPlanningGate_FailedPlansHoldAfterTheLimit(t *testing.T) {
	m, _, prd, calls := planGateFixture(t)
	m.decompose = func(DecomposeRequest) (string, error) {
		calls.Add(1)
		return "", errTestPlanner
	}
	var alerts []string
	m.SetAlertSink(func(level, title, body string) string { alerts = append(alerts, title); return "" })
	for i := 1; i <= autoPlanStartLimit; i++ {
		_, _ = m.DecomposeAutomatic(prd.ID)
		if cur, _ := m.Store().GetPRD(prd.ID); cur.Status != PRDDraft {
			t.Fatalf("after failure %d status %q, want draft", i, cur.Status)
		}
	}
	before := calls.Load()
	_, err := m.DecomposeAutomatic(prd.ID)
	if err == nil || !strings.Contains(err.Error(), "held") {
		t.Fatalf("start %d err = %v, want a hold", autoPlanStartLimit+1, err)
	}
	if calls.Load() != before {
		t.Fatalf("a planner started after the limit: calls %d -> %d", before, calls.Load())
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	if cur.Status != PRDHeld || cur.Hold == nil || !strings.Contains(cur.Hold.Reason, "without an accepted plan") {
		t.Fatalf("status %q hold %+v, want held with the reason", cur.Status, cur.Hold)
	}
	held := 0
	for _, a := range alerts {
		if a == "Automaton held: planning kept failing" {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("hold alerts = %d (all: %v), want exactly one", held, alerts)
	}
	// The hold is the stop: releasing it gives a fresh set of tries.
	if _, err := m.ReleaseHold(prd.ID, "operator", "try again"); err != nil {
		t.Fatalf("release: %v", err)
	}
	m.decompose = func(DecomposeRequest) (string, error) {
		calls.Add(1)
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	if _, err := m.DecomposeAutomatic(prd.ID); err != nil {
		t.Fatalf("an automatic start after the release: %v", err)
	}
	if cur, _ := m.Store().GetPRD(prd.ID); cur.AutoPlanStarts != 0 || cur.Status != PRDNeedsReview {
		t.Fatalf("after an accepted plan: starts %d status %q", cur.AutoPlanStarts, cur.Status)
	}
}

func TestPlanningGate_BootRestartWaitsForTheResume(t *testing.T) {
	m, api, prd, calls := planGateFixture(t)
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	m.SetPauseWaitPoll(5 * time.Millisecond)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	m.store.MutatePRD(prd.ID, func(p *PRD) bool { p.Status = PRDPlanning; return true })

	api.execWG.Add(1)
	api.startPlanningAfterPause(prd.ID)
	time.Sleep(60 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("a restarted planner ran %d time(s) while paused", calls.Load())
	}
	if _, err := ps.Resume(pause.ScopeGlobal, "", "operator", ""); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, 3*time.Second, "planner started after the resume", func() bool { return calls.Load() == 1 })
}

var errTestPlanner = errors.New("planner session failed (test)")

func TestPlanningGate_LaneFillDecomposeHeldWhilePausedAndAfterTheLimit(t *testing.T) {
	h := newLaneFillHarness(t, string(LaneFillModeOn))
	draft := h.addPRD("draft", PRDDraft, "", "")
	ps := newPauseStore(t)
	h.m.SetPauseGate(ps)
	// A pause that begins after the poll chose the draft: the decompose that was
	// about to start is refused by the gate.
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	h.c.lf.mu.Lock()
	rec := h.c.lf.records
	h.c.lf.mu.Unlock()
	e := &laneFillEntry{}
	h.c.laneFillDecompose(rec, e, LaneFillDecision{PRDID: draft.ID, LaneType: LaneTypeClaude})
	h.c.laneFillWait()
	if n := countCallPrefix(h.fake.all(), "decompose:"); n != 0 {
		t.Fatalf("lane fill started %d decompose(s) on a paused project", n)
	}
	_, _ = ps.Resume(pause.ScopeGlobal, "", "operator", "")
	// After the limit of engine starts the Automaton is held, not planned again.
	for i := 0; i < autoPlanStartLimit; i++ {
		h.c.laneFillDecompose(rec, e, LaneFillDecision{PRDID: draft.ID, LaneType: LaneTypeClaude})
		h.c.laneFillWait()
	}
	before := countCallPrefix(h.fake.all(), "decompose:")
	h.c.laneFillDecompose(rec, e, LaneFillDecision{PRDID: draft.ID, LaneType: LaneTypeClaude})
	h.c.laneFillWait()
	if after := countCallPrefix(h.fake.all(), "decompose:"); after != before || before != autoPlanStartLimit {
		t.Fatalf("decompose calls: %d before the extra start, %d after, want %d and no change", before, after, autoPlanStartLimit)
	}
	if cur, _ := h.m.store.GetPRD(draft.ID); cur.Status != PRDHeld {
		t.Fatalf("status after the limit = %q, want held", cur.Status)
	}
}
