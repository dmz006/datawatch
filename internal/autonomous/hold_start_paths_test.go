package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// holdStartLook is the fixed look-again time the tests put on a hold.
var holdStartLook = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)

// holdStartMark puts a stored Automaton on hold the way Manager.Hold leaves
// it: status held and a hold record, with the status it had in From.
func holdStartMark(t *testing.T, m *Manager, id string, from PRDStatus, until *time.Time) {
	t.Helper()
	ok := m.store.MutatePRD(id, func(p *PRD) bool {
		p.Status = PRDHeld
		p.Hold = &HoldState{Reason: "wait for the release", From: from, Until: until, HeldAt: holdClockStart}
		return true
	})
	if !ok {
		t.Fatalf("Automaton %s not found", id)
	}
}

func holdStartDecisionFor(st LaneFillStatus, prdID string) (LaneFillDecision, bool) {
	for _, d := range st.Decisions {
		if d.PRDID == prdID && d.EligibilityReason != "" {
			return d, true
		}
	}
	return LaneFillDecision{}, false
}

func TestHoldLaneFillNeverStartsAHeldAutomaton(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	held := h.addPRD("held work", PRDApproved, "claude", "claude-code")
	until := holdStartLook
	holdStartMark(t, h.m, held.ID, PRDApproved, &until)
	free := h.addPRD("free work", PRDApproved, "claude", "claude-code")

	h.poll()

	for _, c := range h.fake.all() {
		if strings.HasSuffix(c, held.ID) || strings.Contains(c, held.ID+":") {
			t.Fatalf("lane fill called %q for a held Automaton; calls %v", c, h.fake.all())
		}
	}
	if calls := h.fake.all(); len(calls) != 1 || calls[0] != "run:"+free.ID {
		t.Fatalf("calls = %v, want only a run of the non-held Automaton %s", calls, free.ID)
	}
	st := h.status()
	d, ok := holdStartDecisionFor(st, held.ID)
	if !ok {
		t.Fatalf("no refusal decision for the held Automaton in %+v", st.Decisions)
	}
	if d.EligibilityReason != LaneFillReasonOperatorHold || d.Action != LaneFillActionBlocked {
		t.Errorf("decision = %+v, want a blocked decision with code %s", d, LaneFillReasonOperatorHold)
	}
	for _, want := range []string{"wait for the release", "2026-11-20T08:00:00Z"} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("refusal text %q must name %q", d.Reason, want)
		}
	}
	if strings.Contains(d.Reason, "PRD") {
		t.Errorf("operator text must say Automaton, got %q", d.Reason)
	}
}

func TestHoldLaneFillCountsNoHeldAutomatonAsQueued(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	h.c.SetReady()
	held := h.addPRD("held work", PRDApproved, "", "claude-code")
	holdStartMark(t, h.m, held.ID, PRDApproved, nil)

	h.poll()

	st := h.status()
	if st.QueuedEligible.GPU != 0 || st.QueuedEligible.Claude != 0 {
		t.Fatalf("queued = %+v, a held Automaton counts in no total", st.QueuedEligible)
	}
	d, ok := holdStartDecisionFor(st, held.ID)
	if !ok || d.EligibilityReason != LaneFillReasonOperatorHold {
		t.Fatalf("the refusal must show in the status, got %+v", st.Decisions)
	}
	if !strings.Contains(d.Reason, "no date set") {
		t.Errorf("a hold with no look-again time must say so, got %q", d.Reason)
	}

	// An identical Automaton that is not held is counted.
	h.addPRD("free work", PRDApproved, "", "claude-code")
	h.poll()
	if q := h.status().QueuedEligible; q.GPU != 1 || q.Claude != 1 {
		t.Fatalf("queued = %+v, the non-held twin must count once per lane type", q)
	}
}

func TestHoldLaneFillNeitherTerminalNorRunningNorMergeable(t *testing.T) {
	if laneFillTerminal(PRDHeld) || laneFillRunning(PRDHeld) {
		t.Fatal("held is neither terminal nor running for lane fill")
	}
	h := newLaneFillHarness(t, "shadow")
	held := h.addPRD("held finished work", PRDBlocked, "", "claude-code")
	h.addDoneTask(held.ID)
	holdStartMark(t, h.m, held.ID, PRDBlocked, nil)
	cand := LaneFillCandidate{ID: held.ID, Status: PRDHeld, AllTasksDone: true}
	if h.c.laneFillNeedsMerge(context.Background(), nil, cand) {
		t.Fatal("a held Automaton never needs a merge flag")
	}
	// Even a candidate that carries the merge flag is not reported.
	cand.NeedsMergeFlag = true
	if src, _ := ClassifyLaneFillCandidate(cand); src != LaneFillSourceNone {
		t.Fatalf("source = %v, a held candidate is never a merge, draft or ready", src)
	}

	h.c.SetReady()
	h.poll()
	for _, d := range h.status().Decisions {
		if d.PRDID == held.ID && d.Action == LaneFillActionReportMerge {
			t.Fatalf("lane fill reported a merge for a held Automaton: %+v", d)
		}
	}
	if len(h.fake.all()) != 0 {
		t.Fatalf("calls = %v, nothing is started in shadow mode", h.fake.all())
	}
}

func TestHoldLaneFillNeverDecomposesAHeldDraft(t *testing.T) {
	cfg := laneFillCoordConfig("on")
	cfg.LaneFill.DecomposeAheadMin = 2
	h := newLaneFillHarnessIn(t, t.TempDir(), cfg)
	h.c.SetReady()
	held := h.addPRD("held draft", PRDDraft, "claude", "claude-code")
	holdStartMark(t, h.m, held.ID, PRDDraft, nil)

	h.poll()

	for _, c := range h.fake.all() {
		if strings.Contains(c, held.ID) {
			t.Fatalf("lane fill touched a held draft: %v", h.fake.all())
		}
	}
}

func TestHoldEligibilityOperatorHoldComesFirst(t *testing.T) {
	prd := &PRD{ID: "aaaa0001", Title: "HOLD this", Spec: "do not run", Status: PRDHeld}
	code, text := PRDHoldCode(prd, nil)
	if code != LaneFillReasonOperatorHold {
		t.Fatalf("code = %q, a held status wins over a HOLD line", code)
	}
	if !strings.Contains(text, "no reason recorded") || !strings.Contains(text, "no date set") {
		t.Errorf("text = %q", text)
	}
	if LaneFillReasonOperatorHold == LaneFillReasonHeld {
		t.Fatal("operator_hold must differ from the spec hold code")
	}
	prd.Status = PRDApproved
	if code, _ := PRDHoldCode(prd, nil); code != LaneFillReasonHeld {
		t.Fatalf("an approved Automaton with a HOLD line keeps the old code, got %q", code)
	}
}

func TestHoldStartPathsRefuseAHeldAutomaton(t *testing.T) {
	m, _ := newHoldManager(t)
	prd := newOperatorHoldPRD(t, m, PRDNeedsReview, "")
	holdStartMark(t, m, prd.ID, PRDNeedsReview, nil)
	before, _ := m.Store().GetPRD(prd.ID)
	tasksBefore := holdTaskStatuses(before)
	taskID := before.Story[0].Tasks[0].ID

	checks := map[string]func() error{
		"Run": func() error { return m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify) },
		"Approve": func() error {
			_, err := m.Approve(prd.ID, "operator", "")
			return err
		},
		"ResetTask": func() error {
			_, err := m.ResetTask(prd.ID, taskID, "operator", true)
			return err
		},
		"Resume": func() error {
			_, err := m.Resume(prd.ID, "operator")
			return err
		},
		"ApproveStory": func() error {
			_, err := m.ApproveStory(prd.ID, before.Story[0].ID, "operator")
			return err
		},
		"ApproveTask": func() error {
			_, err := m.ApproveTask(prd.ID, taskID, "operator")
			return err
		},
	}
	for name, call := range checks {
		err := call()
		if err == nil {
			t.Errorf("%s: a held Automaton must be refused", name)
			continue
		}
		for _, want := range []string{"held", "wait for the release", "release the hold first"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q must say %q", name, err, want)
			}
		}
		if strings.Contains(err.Error(), "PRD") {
			t.Errorf("%s: operator text must say Automaton, got %q", name, err)
		}
	}

	after, _ := m.Store().GetPRD(prd.ID)
	if after.Status != PRDHeld || after.Hold == nil || after.Hold.From != PRDNeedsReview {
		t.Fatalf("a refused call changed the hold: status %s hold %+v", after.Status, after.Hold)
	}
	if got := holdTaskStatuses(after); len(got) != len(tasksBefore) || got[0] != tasksBefore[0] {
		t.Fatalf("tasks changed: %v -> %v", tasksBefore, got)
	}
	if len(after.Decisions) != len(before.Decisions) {
		t.Fatalf("a refused call recorded a decision: %d -> %d", len(before.Decisions), len(after.Decisions))
	}
}

func TestHoldStartPathsRefuseDecomposeAndReviewRetry(t *testing.T) {
	m, _ := newHoldManager(t)
	prd := newOperatorHoldPRD(t, m, PRDDraft, "")
	holdStartMark(t, m, prd.ID, PRDDraft, nil)
	if _, err := m.Decompose(prd.ID); err == nil || !strings.Contains(err.Error(), "release the hold first") {
		// Decompose may fail first when no decompose function is wired; the
		// guard sits after that check, so wire one and try again.
		m.decompose = fakeDecompose
		if _, err := m.Decompose(prd.ID); err == nil || !strings.Contains(err.Error(), "release the hold first") {
			t.Fatalf("Decompose of a held Automaton = %v, want the held refusal", err)
		}
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDHeld {
		t.Fatalf("status = %s, a refused decompose must not move it to planning", got.Status)
	}

	// A review retry names the held Automaton before it reads the task.
	prd2 := newOperatorHoldPRD(t, m, PRDBlocked, "judge said block")
	tid := prd2.Story[0].Tasks[0].ID
	m.Store().MutateTask(tid, func(tk *Task) { tk.Status = TaskReviewNeeded; tk.ReviewSummary = &ReviewSummary{} })
	holdStartMark(t, m, prd2.ID, PRDBlocked, nil)
	_, err := m.ResolveTaskReview(prd2.ID, tid, ReviewResolution{Action: reviewActionRetry}, "operator")
	if err == nil || !strings.Contains(err.Error(), "release the hold first") {
		t.Fatalf("review retry on a held Automaton = %v, want the held refusal", err)
	}
	if tk, _ := m.Store().GetTask(tid); tk.Status != TaskReviewNeeded {
		t.Fatalf("task status = %s, a refused retry must not requeue it", tk.Status)
	}
}

func TestHoldAPIRunRefusesAHeldAutomaton(t *testing.T) {
	m, _ := newHoldManager(t)
	prd := newOperatorHoldPRD(t, m, PRDApproved, "")
	holdStartMark(t, m, prd.ID, PRDApproved, nil)
	a := &API{M: m}

	if err := a.Run(prd.ID); err == nil || !strings.Contains(err.Error(), "release the hold first") {
		t.Fatalf("API.Run on a held Automaton = %v, want the held refusal", err)
	}
	started, err := a.runLocked(prd.ID, func() bool {
		t.Error("prep must not run for a held Automaton")
		return true
	})
	if started || err == nil || !strings.Contains(err.Error(), "release the hold first") {
		t.Fatalf("runLocked = %v, %v; want not started and the held refusal", started, err)
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDHeld {
		t.Fatalf("status = %s, want held", got.Status)
	}
}

func TestHoldRunningExecutorStopsDispatchingWhenHeld(t *testing.T) {
	// Sequential is the default walk; two at a time takes the concurrent pool.
	for name, concurrency := range map[string]int{"sequential": 1, "concurrent": 2} {
		t.Run(name, func(t *testing.T) {
			m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
			if err != nil {
				t.Fatal(err)
			}
			prd, err := m.CreatePRD("spec", t.TempDir(), "claude", "", EffortNormal)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
				{Title: "first", Spec: "do"},
				{Title: "second", Spec: "do"},
			}}}); err != nil {
				t.Fatal(err)
			}
			m.Store().MutatePRD(prd.ID, func(p *PRD) bool {
				p.Status = PRDApproved
				p.MaxConcurrentTasks = concurrency
				return true
			})
			// The second task waits for the first, so the first runs alone and
			// the hold lands before the second is ready.
			stored, _ := m.Store().GetPRD(prd.ID)
			firstID, secondID := stored.Story[0].Tasks[0].ID, stored.Story[0].Tasks[1].ID
			m.Store().MutateTask(secondID, func(tk *Task) { tk.DependsOn = []string{firstID} })

			var mu sync.Mutex
			var spawned []string
			// The fake worker puts the Automaton on hold while its first task
			// runs, the way an operator hold lands on a record with a live
			// goroutine.
			spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
				mu.Lock()
				spawned = append(spawned, req.TaskID)
				first := len(spawned) == 1
				mu.Unlock()
				if first {
					holdStartMark(t, m, prd.ID, PRDRunning, nil)
				}
				return fakeSpawn(ctx, req)
			}
			if err := m.Run(context.Background(), prd.ID, spawn, fakeVerify); err != nil {
				t.Fatalf("Run: %v", err)
			}

			mu.Lock()
			n := len(spawned)
			mu.Unlock()
			if n != 1 {
				t.Fatalf("spawned %d tasks, a hold must drain the live executor without starting the second", n)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if got.Status != PRDHeld || got.Hold == nil {
				t.Fatalf("status = %s hold %+v, the executor must leave the hold alone", got.Status, got.Hold)
			}
			if st := got.Story[0].Tasks[1].Status; st != TaskPending && st != "" {
				t.Fatalf("second task %s status = %s, want it untouched", secondID, st)
			}
		})
	}
}

func holdStartChain(t *testing.T) (*coordHarness, *Automation) {
	t.Helper()
	h := newCoordHarness(t, 3)
	// m0 completes to m1; a block or a failure of m0 would go to m2.
	a := h.force(h.makeAuto([][3]string{{"m1", "m2", "m2"}}, 3))
	return h, a
}

func TestHoldChainHeldMemberDoesNotAdvanceOrTakeAnEdge(t *testing.T) {
	h, a := holdStartChain(t)
	holdStartMark(t, h.m, h.prds[0].ID, PRDApproved, nil)
	h.event(0, PRDHeld)

	if runs, resumes := h.fake.counts(); len(runs)+len(resumes) != 0 {
		t.Fatalf("starts after a held member: runs %v resumes %v", runs, resumes)
	}
	got := h.get(a)
	if got.Status != AutomationActive || got.CurrentMemberID != "m0" || got.IdleSince != nil {
		t.Fatalf("status=%s cursor=%q idle=%v, the Automation keeps its own status", got.Status, got.CurrentMemberID, got.IdleSince)
	}
	m0 := got.Members[0]
	if m0.Status != MemberRunning || m0.EdgeTaken != "" || m0.FinishedAt != nil || m0.Outcome != "" {
		t.Fatalf("held member must be neither finished nor failed nor blocked: %+v", m0)
	}
	if got.Members[1].Status == MemberRunning || got.Members[2].Status == MemberRunning {
		t.Fatalf("a later member started: %+v", got.Members)
	}
	for _, kind := range []string{"advance", "blocked"} {
		if hasCoordDecision(got.Decisions, kind) {
			t.Errorf("unexpected %q decision for a held member", kind)
		}
	}
	if h.alertCount("") != 0 {
		t.Errorf("a held member raised alerts: %v", h.alerts)
	}

	// The reconcile pass changes nothing for a held member either.
	h.c.reconcile()
	if again := h.get(a); again.Status != AutomationActive || again.CurrentMemberID != "m0" || again.Version != got.Version {
		t.Fatalf("reconcile moved a held member: %+v", again)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("reconcile started %v for a held member", runs)
	}
}

func TestHoldChainAdvancesAfterReleaseAndCompletion(t *testing.T) {
	h, a := holdStartChain(t)
	holdStartMark(t, h.m, h.prds[0].ID, PRDApproved, nil)
	h.event(0, PRDHeld)
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("started %v while held", runs)
	}

	// The operator releases the hold and the member then completes.
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	runs, _ := h.fake.counts()
	if len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs = %v, want the next member %s", runs, h.prds[1].ID)
	}
	got := h.get(a)
	if got.CurrentMemberID != "m1" || got.Status != AutomationActive {
		t.Fatalf("cursor=%q status=%s", got.CurrentMemberID, got.Status)
	}
	if m0 := got.Members[0]; m0.Status != MemberCompleted || m0.EdgeTaken != "on_complete" {
		t.Fatalf("member m0 = %+v, want completed by on_complete", m0)
	}
}

func TestHoldChainNextMemberHeldWaitsWithoutBlocking(t *testing.T) {
	h, a := holdStartChain(t)
	holdStartMark(t, h.m, h.prds[1].ID, PRDApproved, nil)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("started %v although the next member is held", runs)
	}
	got := h.get(a)
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, a held next member must not block the Automation", got.Status)
	}
	if got.CurrentMemberID != "m1" || got.Members[1].Status == MemberRunning || got.Members[1].Status == MemberFailed {
		t.Fatalf("cursor=%q member m1=%+v, it waits at the cursor, not started", got.CurrentMemberID, got.Members[1])
	}
	if !hasCoordDecision(got.Decisions, "held") || hasCoordDecision(got.Decisions, "blocked") {
		t.Fatalf("decisions = %+v, want a held note and no blocked", got.Decisions)
	}

	// A pass while it is still held starts nothing and blocks nothing.
	h.c.reconcile()
	if again := h.get(a); again.Status != AutomationActive {
		t.Fatalf("reconcile blocked the Automation for a held member: %s", again.Status)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("reconcile started %v for a held member", runs)
	}

	// Released back to approved, the next pass starts it.
	h.setStatus(1, PRDApproved)
	h.c.reconcile()
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs = %v after the release, want %s", runs, h.prds[1].ID)
	}
}

func TestHoldChainMemberEndMapsHeldAsWaiting(t *testing.T) {
	if _, ended := prdEnd(PRDHeld); ended {
		t.Fatal("a held member has not ended")
	}
	if mode := gateMode(PRDHeld); mode != "" {
		t.Fatalf("gate mode = %q, a held member is never started", mode)
	}
	found := false
	for _, k := range coordinatorKinds() {
		if k == PRDStatusKind(PRDHeld) {
			found = true
		}
	}
	if !found {
		t.Fatal("the coordinator must listen to the held status event")
	}
}
