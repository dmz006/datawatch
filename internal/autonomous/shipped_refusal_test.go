package autonomous

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

// shippedRelease and shippedCommit are the shipped record the refusal tests
// put on an Automaton: both fields set, the way the merge step leaves them.
const (
	shippedRelease = "v9.183.0"
	shippedCommit  = "77304085e0b3c2f1a9d8e7c6b5a4f3e2d1c0b9a8"
)

// newShippedPRD stores an Automaton with one pending task, forced to status,
// carrying the given shipped record (empty strings leave a field unset).
func newShippedPRD(t *testing.T, m *Manager, title string, status PRDStatus, release, commit string) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("spec "+title, t.TempDir(), "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "work", Spec: "do"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	if !m.Store().MutatePRD(prd.ID, func(p *PRD) bool {
		p.Title, p.Status = title, status
		p.MergedInRelease, p.MergeCommit = release, commit
		return true
	}) {
		t.Fatalf("Automaton %s not found", prd.ID)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	return got
}

func shippedDecisions(p *PRD, kind string) []Decision {
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

// A queued Automaton whose own record says its work shipped is refused at
// start, whichever door it comes through, and ends delivered (completed),
// never cancelled. No worker is spawned.
func TestShippedQueuedAutomatonIsRefusedAtStartAndMarkedDelivered(t *testing.T) {
	for _, status := range []PRDStatus{PRDApproved, PRDNeedsReview, PRDDraft} {
		t.Run(string(status), func(t *testing.T) {
			m, _ := newHoldManager(t)
			prd := newShippedPRD(t, m, "Land the thing onto main", status, shippedRelease, shippedCommit)

			var spawned atomic.Int32
			spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
				spawned.Add(1)
				return fakeSpawn(ctx, req)
			}
			err := m.Run(context.Background(), prd.ID, spawn, fakeVerify)
			if err == nil {
				t.Fatalf("Run of a shipped %s Automaton must be refused", status)
			}
			for _, want := range []string{"already shipped", shippedRelease} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q must name %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "PRD") {
				t.Errorf("operator text must say Automaton, got %q", err)
			}
			if n := spawned.Load(); n != 0 {
				t.Fatalf("spawned %d workers for shipped work", n)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if got.Status != PRDCompleted {
				t.Fatalf("status = %s, shipped work is marked delivered (completed), never cancelled", got.Status)
			}
			ds := shippedDecisions(got, "shipped_refusal")
			if len(ds) != 1 {
				t.Fatalf("want one shipped_refusal decision, got %d in %+v", len(ds), got.Decisions)
			}
			for _, want := range []string{shippedRelease, shippedCommit[:9], "delivered"} {
				if !strings.Contains(ds[0].Note, want) {
					t.Errorf("recorded reason %q must name %q", ds[0].Note, want)
				}
			}
			// A second attempt is still refused and records nothing more.
			if err := m.Run(context.Background(), prd.ID, spawn, fakeVerify); err == nil {
				t.Fatal("second Run of a delivered Automaton must be refused")
			}
			again, _ := m.Store().GetPRD(prd.ID)
			if n := len(shippedDecisions(again, "shipped_refusal")); n != 1 {
				t.Fatalf("a repeat refusal recorded %d decisions, want still 1", n)
			}
		})
	}
}

// The doors toward a start (approve, decompose, the API run) refuse too.
func TestShippedAutomatonIsRefusedAtApproveDecomposeAndAPIRun(t *testing.T) {
	m, _ := newHoldManager(t)
	m.decompose = fakeDecompose
	approvable := newShippedPRD(t, m, "approvable", PRDNeedsReview, shippedRelease, shippedCommit)
	if _, err := m.Approve(approvable.ID, "operator", ""); err == nil || !strings.Contains(err.Error(), "already shipped") {
		t.Fatalf("Approve of shipped work = %v, want the shipped refusal", err)
	}
	if got, _ := m.Store().GetPRD(approvable.ID); got.Status == PRDApproved {
		t.Fatal("shipped work must not become approved")
	}

	draft := newShippedPRD(t, m, "draft", PRDDraft, shippedRelease, shippedCommit)
	if _, err := m.Decompose(draft.ID); err == nil || !strings.Contains(err.Error(), "already shipped") {
		t.Fatalf("Decompose of shipped work = %v, want the shipped refusal", err)
	}
	if got, _ := m.Store().GetPRD(draft.ID); got.Status == PRDPlanning {
		t.Fatal("shipped work must not enter planning")
	}

	queued := newShippedPRD(t, m, "queued", PRDApproved, shippedRelease, shippedCommit)
	a := &API{M: m}
	if err := a.Run(queued.ID); err == nil || !strings.Contains(err.Error(), "already shipped") {
		t.Fatalf("API.Run of shipped work = %v, want the shipped refusal", err)
	}
	if got, _ := m.Store().GetPRD(queued.ID); got.Status == PRDRunning {
		t.Fatal("API.Run must not flip shipped work to running")
	}
}

// Archived, cancelled and completed Automata are closed: refused at start with
// the reason on the record, and the status is left as it was.
func TestClosedAutomatonIsRefusedAtStartWithTheReasonRecorded(t *testing.T) {
	for _, status := range []PRDStatus{PRDArchived, PRDCancelled, PRDCompleted} {
		t.Run(string(status), func(t *testing.T) {
			m, _ := newHoldManager(t)
			prd := newShippedPRD(t, m, "closed work", status, "", "")
			a := &API{M: m}

			err := a.Run(prd.ID)
			if err == nil || !strings.Contains(err.Error(), string(status)) || !strings.Contains(err.Error(), "never runs") {
				t.Fatalf("API.Run of a %s Automaton = %v, want a refusal naming the status and that closed work never runs", status, err)
			}
			if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err == nil || !strings.Contains(err.Error(), "never runs") {
				t.Fatalf("Manager.Run of a %s Automaton = %v, want the closed-work refusal", status, err)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if got.Status != status {
				t.Fatalf("status = %s, a refusal leaves a closed Automaton as it was (%s)", got.Status, status)
			}
			if n := len(shippedDecisions(got, "closed_work_refused")); n != 1 {
				t.Fatalf("want exactly one closed_work_refused decision after two refusals, got %d in %+v", n, got.Decisions)
			}
		})
	}
}

// A cancelled Automaton with a shipped record is refused and stays cancelled:
// the sweep marks queued work delivered, it never rewrites closed work.
func TestCancelledShippedAutomatonStaysCancelled(t *testing.T) {
	m, _ := newHoldManager(t)
	prd := newShippedPRD(t, m, "cancelled but shipped", PRDCancelled, shippedRelease, shippedCommit)
	err := (&API{M: m}).Run(prd.ID)
	if err == nil {
		t.Fatal("a cancelled Automaton must not start")
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}

// Lane fill never picks shipped work and the daemon drops it from the queue:
// a draft, a needs_review and an approved Automaton with a shipped record are
// all marked delivered after one poll, and only the unshipped twin starts.
func TestLaneFillDropsShippedAutomataFromTheQueue(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	landing := h.addPRD("Land the pi stall work on main", PRDNeedsReview, "claude", "claude-code")
	approved := h.addPRD("approved and shipped", PRDApproved, "claude", "claude-code")
	draft := h.addPRD("draft and shipped", PRDDraft, "claude", "claude-code")
	free := h.addPRD("not shipped", PRDApproved, "claude", "claude-code")
	for _, p := range []*PRD{landing, approved, draft} {
		h.m.Store().MutatePRD(p.ID, func(p *PRD) bool {
			p.MergedInRelease, p.MergeCommit = shippedRelease, shippedCommit
			return true
		})
	}

	h.poll()

	for _, c := range h.fake.all() {
		for _, p := range []*PRD{landing, approved, draft} {
			if strings.Contains(c, p.ID) {
				t.Fatalf("lane fill called %q for shipped work; calls %v", c, h.fake.all())
			}
		}
	}
	if calls := h.fake.all(); len(calls) != 1 || calls[0] != "run:"+free.ID {
		t.Fatalf("calls = %v, want only a run of the unshipped Automaton %s", calls, free.ID)
	}
	for _, p := range []*PRD{landing, approved, draft} {
		got, _ := h.m.Store().GetPRD(p.ID)
		if got.Status != PRDCompleted {
			t.Errorf("Automaton %s status = %s, shipped queued work is dropped as delivered (completed)", p.ID, got.Status)
		}
		if n := len(shippedDecisions(got, "shipped_refusal")); n != 1 {
			t.Errorf("Automaton %s: want one shipped_refusal decision, got %d", p.ID, n)
		}
	}
	// The refusal shows in the lane fill status with its code and the release,
	// before the Automaton is dropped, so the operator sees why.
	st := h.status()
	for _, p := range []*PRD{landing, approved, draft} {
		d, ok := holdStartDecisionFor(st, p.ID)
		if !ok {
			t.Errorf("no refusal decision for shipped Automaton %s in %+v", p.ID, st.Decisions)
			continue
		}
		if d.EligibilityReason != LaneFillReasonAlreadyShipped || d.Action != LaneFillActionBlocked {
			t.Errorf("decision = %+v, want a blocked decision with code %s", d, LaneFillReasonAlreadyShipped)
		}
		if !strings.Contains(d.Reason, shippedRelease) {
			t.Errorf("refusal text %q must name the release %s", d.Reason, shippedRelease)
		}
	}
}

// The sweep drops queued work only: a held Automaton keeps its deliberate
// hold and a running one keeps its lane, even with a shipped record.
func TestShippedSweepLeavesHeldAndRunningAlone(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	h.c.SetReady()
	held := h.addPRD("held landing wrapper", PRDApproved, "claude", "claude-code")
	holdStartMark(t, h.m, held.ID, PRDApproved, nil)
	running := h.addPRD("running work", PRDRunning, "claude", "claude-code")
	for _, p := range []*PRD{held, running} {
		h.m.Store().MutatePRD(p.ID, func(p *PRD) bool {
			p.MergedInRelease, p.MergeCommit = shippedRelease, shippedCommit
			return true
		})
	}

	h.poll()

	if got, _ := h.m.Store().GetPRD(held.ID); got.Status != PRDHeld {
		t.Errorf("held Automaton status = %s, an operator hold is never overwritten", got.Status)
	}
	if got, _ := h.m.Store().GetPRD(running.ID); got.Status != PRDRunning {
		t.Errorf("running Automaton status = %s, the sweep never touches a live run", got.Status)
	}
}

// The check is on the Automaton's own record. An Automaton that is sequenced
// after shipped work, or sits beside it, is not blocked by that record.
func TestShippedRecordOnAnotherEntityDoesNotBlock(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	first := h.addPRD("First part", PRDCompleted, "claude", "claude-code")
	h.m.Store().MutatePRD(first.ID, func(p *PRD) bool {
		p.MergedInRelease, p.MergeCommit = shippedRelease, shippedCommit
		return true
	})
	after := h.addPRD("Second part", PRDApproved, "claude", "claude-code")
	h.m.Store().MutatePRD(after.ID, func(p *PRD) bool {
		p.Spec = "Sequenced after First part."
		return true
	})

	h.poll()

	if calls := h.fake.all(); len(calls) != 1 || calls[0] != "run:"+after.ID {
		t.Fatalf("calls = %v, the dependent of shipped work must still run", calls)
	}
	got, _ := h.m.Store().GetPRD(after.ID)
	if n := len(shippedDecisions(got, "shipped_refusal")); n != 0 {
		t.Fatalf("the dependent got %d shipped_refusal decisions, want 0", n)
	}
	if err := (&API{M: h.m}).Run(after.ID); err != nil {
		t.Fatalf("API.Run of the dependent: %v", err)
	}
}

// A commit with no release (an unreleased merge) is not the shipped record the
// rule is built on, and a missing record never refuses.
func TestOnlyACompleteShippedRecordRefuses(t *testing.T) {
	m, _ := newHoldManager(t)
	noRelease := newShippedPRD(t, m, "merged not released", PRDApproved, "", shippedCommit)
	if err := (&API{M: m}).Run(noRelease.ID); err != nil {
		t.Fatalf("a commit with no release must not be refused as shipped: %v", err)
	}
	plain := newShippedPRD(t, m, "plain", PRDApproved, "", "")
	if err := (&API{M: m}).Run(plain.ID); err != nil {
		t.Fatalf("an Automaton with no shipped record must run: %v", err)
	}
}
