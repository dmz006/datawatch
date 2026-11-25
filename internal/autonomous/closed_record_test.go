// Closed records stay closed through restarts and recovery passes: no boot-time
// or periodic recovery may touch a cancelled, rejected or archived Automaton,
// or one with a recorded closed outcome.

package autonomous

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// closedRecordCases are the four ways a record is closed to recovery. The last
// two carry a status that on its own would look open.
var closedRecordCases = []struct {
	name    string
	status  PRDStatus
	outcome string
}{
	{"cancelled", PRDCancelled, ""},
	{"rejected", PRDRejected, ""},
	{"archived", PRDArchived, ""},
	{"completed with an outcome", PRDCompleted, OutcomeDelivered},
	{"blocked with an outcome", PRDBlocked, OutcomeSuperseded},
}

// closeRecord writes the closed state straight onto the record, as an earlier
// daemon left it: the status, the outcome and the closing decision.
func closeRecord(t *testing.T, m *Manager, id string, status PRDStatus, outcome string) {
	t.Helper()
	p, ok := m.Store().GetPRD(id)
	if !ok {
		t.Fatalf("prd %s gone", id)
	}
	at := time.Now()
	m.Store().WithLock(func() {
		p.Status = status
		if status == PRDCancelled {
			p.Decisions = append(p.Decisions, Decision{At: at, Kind: "cancel", Actor: "operator"})
		}
		if outcome != "" {
			applyClosedOutcome(p, outcome, "", "recorded by the test", at)
			p.Decisions = append(p.Decisions, Decision{At: at, Kind: DecisionClosedOutcome, Actor: "operator"})
		}
	})
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
}

func decisionCount(t *testing.T, m *Manager, id, kind string) int {
	t.Helper()
	p, ok := m.Store().SnapshotPRD(id)
	if !ok {
		t.Fatalf("prd %s gone", id)
	}
	n := 0
	for _, d := range p.Decisions {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

// The defect: a cancelled Automaton with a held protected-paths task survives a
// restart pass with no release, no reset and no run decision.
func TestRestartPassLeavesCancelledProtectedPathsHoldAlone(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	questionID, _ := fpfHold(t, m, prd, task, repo, dscDaemonOnlyRules())
	closeRecord(t, m, prd.ID, PRDCancelled, OutcomeSuperseded)

	api := NewAPI(m)
	var spawned atomic.Int32
	api.spawnFn = func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	api.verify = func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api.resumeRunningPRDs()
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}

	got, _ := m.Store().SnapshotPRD(prd.ID)
	if got.Status != PRDCancelled {
		t.Errorf("status = %q after the restart pass; want cancelled", got.Status)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil {
		t.Errorf("task = status %q hold %+v; want it left blocked and held", tk.Status, tk.ProtectedPathHold)
	}
	for _, kind := range []string{protectedPathsFalsePositiveDecisionKind, "reset_task", "run"} {
		if n := decisionCount(t, m, prd.ID, kind); n != 0 {
			t.Errorf("%d %q decision(s) on a cancelled record; want none", n, kind)
		}
	}
	if q, ok := store.Get(questionID); !ok || q.State != QuestionOpen {
		t.Errorf("hold question = %+v; the pass must not withdraw it", q)
	}
	if spawned.Load() != 0 {
		t.Errorf("spawned %d worker(s) for a cancelled record", spawned.Load())
	}
}

// Every kind of closed record is skipped by the false-positive release.
func TestFalsePositiveReleaseSkipsEveryClosedRecord(t *testing.T) {
	for _, tc := range closedRecordCases {
		t.Run(tc.name, func(t *testing.T) {
			m, prd, task, _, repo := fpfRig(t)
			fpfHold(t, m, prd, task, repo, dscDaemonOnlyRules())
			closeRecord(t, m, prd.ID, tc.status, tc.outcome)
			if got := m.ReleaseFalseProtectedPathHolds(context.Background()); len(got) != 0 {
				t.Fatalf("released = %v on a closed record", got)
			}
			if p, _ := m.Store().SnapshotPRD(prd.ID); p.Status != tc.status {
				t.Errorf("status = %q; want %q", p.Status, tc.status)
			}
		})
	}
}

// revivedRecord is the state the defect left behind: closed, then released,
// reset and run again by the daemon, reading running with the outcome set.
func revivedRecord(t *testing.T, m *Manager, proj string) *PRD {
	t.Helper()
	prd := gatePRD(t, m, proj, time.Minute)
	closeRecord(t, m, prd.ID, PRDCancelled, OutcomeSuperseded)
	p, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() {
		p.Status = PRDRunning
		p.Decisions = append(p.Decisions,
			Decision{At: time.Now(), Kind: protectedPathsFalsePositiveDecisionKind, Actor: "autonomous"},
			Decision{At: time.Now(), Kind: "reset_task", Actor: "autonomous"},
			Decision{At: time.Now(), Kind: "run", Actor: "autonomous"})
	})
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return p
}

// Boot resume never selects a record with a closed outcome, and puts one that
// reads running back to its closed status with a decision naming why.
func TestBootResumeRestoresAndNeverSelectsClosedOutcome(t *testing.T) {
	m, api := gateFixture(t)
	prd := revivedRecord(t, m, testProjectDir(t))
	var spawned atomic.Int32
	api.spawnFn = func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	api.resumeRunningPRDs()
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}

	got, _ := m.Store().SnapshotPRD(prd.ID)
	if got.Status != PRDCancelled {
		t.Errorf("status = %q; want cancelled restored", got.Status)
	}
	if got.ClosedOutcome != OutcomeSuperseded {
		t.Errorf("closed outcome = %q; want superseded kept", got.ClosedOutcome)
	}
	if spawned.Load() != 0 {
		t.Errorf("spawned %d worker(s) for a record with a closed outcome", spawned.Load())
	}
	if rep := m.BootResumeReport(); len(rep) != 0 {
		t.Errorf("boot resume report = %+v; a closed record is neither resumed nor parked", rep)
	}
	var note string
	for _, d := range got.Decisions {
		if d.Kind == "closed_record_restored" {
			note = d.Note
		}
	}
	if !strings.Contains(note, "superseded") || !strings.Contains(note, "cancelled") {
		t.Errorf("restore decision note = %q; want it to name the outcome and the restored status", note)
	}

	// A second restart changes nothing more.
	api.resumeRunningPRDs()
	if n := decisionCount(t, m, prd.ID, "closed_record_restored"); n != 1 {
		t.Errorf("restore decisions = %d after two passes; want 1", n)
	}
}

// A failed record with a closed outcome is not recovered either.
func TestBootResumeSkipsFailedAndPlanningRecordsWithOutcome(t *testing.T) {
	for _, status := range []PRDStatus{PRDFailed, PRDPlanning} {
		t.Run(string(status), func(t *testing.T) {
			m, api := gateFixture(t)
			prd := gatePRD(t, m, testProjectDir(t), time.Minute)
			closeRecord(t, m, prd.ID, status, OutcomeDropped)
			before, _ := m.Store().SnapshotPRD(prd.ID)
			api.resumeRunningPRDs()
			if err := api.WaitIdle(10 * time.Second); err != nil {
				t.Fatal(err)
			}
			got, _ := m.Store().SnapshotPRD(prd.ID)
			if got.Status != status || len(got.Decisions) != len(before.Decisions) {
				t.Errorf("status = %q, decisions %d -> %d; want the record untouched", got.Status, len(before.Decisions), len(got.Decisions))
			}
		})
	}
}

// The restore reads the closed status from the record's own log.
func TestRestoreClosedStatusFollowsTheRecord(t *testing.T) {
	cases := []struct {
		name    string
		log     []string
		outcome string
		want    PRDStatus
	}{
		{"cancel", []string{"cancel", DecisionClosedOutcome}, OutcomeDropped, PRDCancelled},
		{"reject", []string{"reject", DecisionClosedOutcome}, OutcomeDropped, PRDRejected},
		{"archive after cancel", []string{"cancel", "archive"}, OutcomeSuperseded, PRDArchived},
		{"delivered with no closing entry", []string{DecisionClosedOutcome}, OutcomeDelivered, PRDCompleted},
		{"dropped with no closing entry", []string{DecisionClosedOutcome}, OutcomeDropped, PRDCancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, api := gateFixture(t)
			prd := gatePRD(t, m, testProjectDir(t), time.Minute)
			p, _ := m.Store().GetPRD(prd.ID)
			m.Store().WithLock(func() {
				applyClosedOutcome(p, tc.outcome, "", "", time.Now())
				for _, k := range tc.log {
					p.Decisions = append(p.Decisions, Decision{At: time.Now(), Kind: k, Actor: "someone"})
				}
			})
			if err := m.Store().SavePRD(p); err != nil {
				t.Fatal(err)
			}
			api.resumeRunningPRDs()
			if err := api.WaitIdle(10 * time.Second); err != nil {
				t.Fatal(err)
			}
			if got, _ := m.Store().SnapshotPRD(prd.ID); got.Status != tc.want {
				t.Errorf("restored status = %q; want %q", got.Status, tc.want)
			}
		})
	}
}

// Lane fill never selects a record with a closed outcome: not as work to
// start, not as a draft to plan and not as the occupant of a lane.
func TestLaneFillNeverSelectsClosedOutcome(t *testing.T) {
	h := newLaneFillHarness(t, "")
	h.c.SetReady()
	closedReady := h.addPRD("closed ready work", PRDApproved, "gpu", "claude-code")
	closedRunning := h.addPRD("closed running work", PRDRunning, "claude", "claude-code")
	closedDraft := h.addPRD("closed draft", PRDDraft, "", "")
	closedReview := h.addPRD("closed review", PRDNeedsReview, "claude", "claude-code")
	for _, p := range []*PRD{closedReady, closedRunning, closedDraft, closedReview} {
		closeRecord(t, h.m, p.ID, p.Status, OutcomeSuperseded)
	}

	in, st := h.c.laneFillSnapshot(context.Background(), h.m.Config().LaneFill, h.now)
	closed := map[string]bool{closedReady.ID: true, closedRunning.ID: true, closedDraft.ID: true, closedReview.ID: true}
	for _, c := range in.Candidates {
		if closed[c.ID] {
			t.Errorf("closed record %q is a lane fill candidate", c.Title)
		}
	}
	for _, l := range in.Lanes {
		if l.Occupant != nil && closed[l.Occupant.PRDID] {
			t.Errorf("closed record %q occupies lane %s", l.Occupant.Title, l.ID)
		}
	}
	for _, w := range st.Waiting {
		if closed[w.PRDID] {
			t.Errorf("closed record %q is listed as a waiting occupant", w.Title)
		}
	}

	h.poll()
	for _, d := range h.status().Decisions {
		if closed[d.PRDID] {
			t.Errorf("lane fill decided %+v on a closed record", d)
		}
	}
}

// No door starts a record with a closed outcome, whatever its status says, and
// the rule does not depend on the shipped refusal setting.
func TestRunRefusesClosedOutcomeWhateverTheStatus(t *testing.T) {
	for _, mode := range []string{ShippedRefusalOn, ShippedRefusalShadow, ShippedRefusalOff} {
		for _, status := range []PRDStatus{PRDApproved, PRDRunning} {
			t.Run(mode+"/"+string(status), func(t *testing.T) {
				m, api := gateFixture(t)
				m.mu.Lock()
				m.cfg.ShippedRefusal = mode
				m.mu.Unlock()
				prd := gatePRD(t, m, testProjectDir(t), time.Minute)
				closeRecord(t, m, prd.ID, status, OutcomeSuperseded)
				err := api.Run(prd.ID)
				if err == nil || !strings.Contains(err.Error(), "closed") {
					t.Fatalf("Run = %v; want a closed record refusal", err)
				}
				if err := api.WaitIdle(10 * time.Second); err != nil {
					t.Fatal(err)
				}
				if got, _ := m.Store().SnapshotPRD(prd.ID); got.Status != status {
					t.Errorf("status = %q; want %q untouched", got.Status, status)
				}
			})
		}
	}
}

// A task reset by anyone but the operator is refused on a closed record.
func TestResetByRecoveryRefusedOnClosedRecord(t *testing.T) {
	for _, actor := range []string{"autonomous", "housekeeping-repair", "supervisor-loop", "session:abc", "doer"} {
		t.Run(actor, func(t *testing.T) {
			m, prd, task, _, repo := fpfRig(t)
			fpfHold(t, m, prd, task, repo, dscDaemonOnlyRules())
			closeRecord(t, m, prd.ID, PRDCancelled, OutcomeSuperseded)
			for _, force := range []bool{false, true} {
				if _, err := m.ResetTask(prd.ID, task.ID, actor, force); err == nil {
					t.Fatalf("reset (force=%v) by %q succeeded on a closed record", force, actor)
				}
			}
			got, _ := m.Store().SnapshotPRD(prd.ID)
			if got.Status != PRDCancelled || got.Story[0].Tasks[0].Status != TaskBlocked {
				t.Errorf("status = %q task = %q; want both untouched", got.Status, got.Story[0].Tasks[0].Status)
			}
		})
	}
}

// The operator's own reset still reopens a cancelled record. It clears the
// outcome, so no later restart puts the record back.
func TestOperatorResetReopensAndClearsOutcome(t *testing.T) {
	m, prd, task, _, repo := fpfRig(t)
	fpfHold(t, m, prd, task, repo, dscDaemonOnlyRules())
	closeRecord(t, m, prd.ID, PRDCancelled, OutcomeDropped)

	if _, err := m.ResetTask(prd.ID, task.ID, "operator", false); err != nil {
		t.Fatalf("operator reset: %v", err)
	}
	got, _ := m.Store().SnapshotPRD(prd.ID)
	if got.Status != PRDRunning || got.ClosedOutcome != "" || got.ClosedOutcomeAt != nil {
		t.Fatalf("status = %q outcome = %q; want running with the outcome cleared", got.Status, got.ClosedOutcome)
	}
	if n := decisionCount(t, m, prd.ID, "closed_outcome_cleared"); n != 1 {
		t.Errorf("closed_outcome_cleared decisions = %d; want 1", n)
	}
}

// Reset to draft is the operator's own reopen too.
func TestResetToDraftClearsOutcome(t *testing.T) {
	m, api := gateFixture(t)
	prd := gatePRD(t, m, testProjectDir(t), time.Minute)
	closeRecord(t, m, prd.ID, PRDCancelled, OutcomeDropped)
	if _, err := api.ResetToDraft(prd.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Store().SnapshotPRD(prd.ID)
	if got.Status != PRDDraft || got.ClosedOutcome != "" {
		t.Errorf("status = %q outcome = %q; want draft with the outcome cleared", got.Status, got.ClosedOutcome)
	}
}

// A record the operator reopened before this rule existed still carries its old
// outcome. The restart must not undo the operator's reopen.
func TestRestartKeepsAnEarlierOperatorReopen(t *testing.T) {
	m, api := gateFixture(t)
	prd := gatePRD(t, m, testProjectDir(t), time.Minute)
	closeRecord(t, m, prd.ID, PRDCancelled, OutcomeDropped)
	p, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() {
		p.Status = PRDRunning
		p.Decisions = append(p.Decisions, Decision{At: time.Now(), Kind: "reset_task", Actor: "operator"})
	})
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	if got := m.RestoreClosedRecords(); len(got) != 0 {
		t.Fatalf("restored = %v; the operator reopened this record", got)
	}
	_ = api
}

// The boot reconcile, the stuck-running watchdog and the timed merge retry
// leave closed records alone.
func TestPeriodicPassesSkipClosedRecords(t *testing.T) {
	for _, tc := range closedRecordCases {
		t.Run("merge retry/"+tc.name, func(t *testing.T) {
			p := &PRD{ID: "p", ProjectDir: "/p", Status: tc.status, ClosedOutcome: tc.outcome,
				Story: []Story{{Tasks: []Task{{ID: "t", Status: TaskCompleted, WorktreeBranch: "b"}}}}}
			if mergeRetryEligible(p) {
				t.Error("a closed record is eligible for a merge retry")
			}
		})
	}
	t.Run("boot reconcile", func(t *testing.T) {
		m, _ := gateFixture(t)
		prd := gatePRD(t, m, testProjectDir(t), time.Minute)
		p, _ := m.Store().GetPRD(prd.ID)
		m.Store().WithLock(func() { p.Story[0].Tasks[0].Status = TaskInProgress })
		closeRecord(t, m, prd.ID, PRDRunning, OutcomeSuperseded)
		m.reconcileStuckTasks()
		got, _ := m.Store().SnapshotPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskInProgress || decisionCount(t, m, prd.ID, "boot_reconcile") != 0 {
			t.Errorf("task = %q; boot reconcile reset a task of a closed record", got.Story[0].Tasks[0].Status)
		}
	})
}

// The repair passes find nothing to do on a closed record.
func TestRepairPassesSkipClosedRecords(t *testing.T) {
	for _, tc := range closedRecordCases {
		t.Run(tc.name, func(t *testing.T) {
			m, api := gateFixture(t)
			prd := gatePRD(t, m, testProjectDir(t), time.Minute)
			p, _ := m.Store().GetPRD(prd.ID)
			var taskID string
			m.Store().WithLock(func() {
				tk := &p.Story[0].Tasks[0]
				taskID = tk.ID
				tk.Status = TaskReviewNeeded
				tk.FailureReason = &FailureReason{Source: infraRetryHoldSource}
			})
			closeRecord(t, m, prd.ID, tc.status, tc.outcome)
			ctx := context.Background()

			for _, a := range (ManagerAdvanceOps{API: api}).Automata(ctx) {
				if a.ID == prd.ID {
					t.Error("the advance repair lists a closed record")
				}
			}
			for _, c := range (ManagerChainOps{API: api}).ChainTasks(ctx) {
				if c.AutomatonID == prd.ID {
					t.Error("the task chain repair lists a task of a closed record")
				}
			}
			for _, d := range (ManagerPlanningOps{API: api}).Drafts(ctx) {
				if d.ID == prd.ID {
					t.Error("the planning repair lists a closed record")
				}
			}
			if _, err := m.ResolveTaskReview(prd.ID, taskID, ReviewResolution{Action: reviewActionAccept}, "supervisor-loop"); err == nil {
				t.Error("a task review of a closed record was resolved by the daemon")
			}
		})
	}
}

// A restart never adopts a verifier of a closed record: it ends it.
func TestVerifierReconcileEndsVerifierOfClosedRecord(t *testing.T) {
	m, _ := gateFixture(t)
	prd := gatePRD(t, m, testProjectDir(t), time.Minute)
	p, _ := m.Store().GetPRD(prd.ID)
	var taskID string
	m.Store().WithLock(func() {
		taskID = p.Story[0].Tasks[0].ID
		p.Story[0].Tasks[0].Status = TaskVerifying
	})
	closeRecord(t, m, prd.ID, PRDCancelled, OutcomeSuperseded)
	host := &fakeVerifierHost{live: []VerifierSession{{ID: "verify-1", TaskID: taskID}}, ended: map[string]string{}, bound: map[string]string{}}
	m.ReconcileVerifiers(host)
	if _, ok := host.ended["verify-1"]; !ok || len(host.bound) != 0 {
		t.Errorf("ended = %v bound = %v; want the verifier ended and no lease bound", host.ended, host.bound)
	}
	if tk, _ := m.Store().GetTask(taskID); tk.VerifierSessionID != "" {
		t.Errorf("verifier %q adopted onto a task of a closed record", tk.VerifierSessionID)
	}
}

// The gate repair, the held question pass, the stuck-running watchdog and the
// shipped queue drop leave closed records alone.
func TestMorePassesSkipClosedRecords(t *testing.T) {
	t.Run("gate repair", func(t *testing.T) {
		m, _ := gateFixture(t)
		prd := gatePRD(t, m, testProjectDir(t), time.Minute)
		p, _ := m.Store().GetPRD(prd.ID)
		m.Store().WithLock(func() { p.Story[0].Tasks[0].Status = TaskFailed })
		closeRecord(t, m, prd.ID, PRDCancelled, OutcomeSuperseded)
		WireGateRepair(m, func() config.Config { return config.Config{} }, GateRepairWiring{
			TaskGate: func(string, string) (GateRepairInput, bool) { return GateRepairInput{}, false },
		})
		if out := m.RunGateRepairPass(context.Background()); len(out) != 0 {
			t.Errorf("gate repair ran on %d task(s) of a closed record", len(out))
		}
	})
	t.Run("shipped queue drop", func(t *testing.T) {
		m, _ := gateFixture(t)
		prd := gatePRD(t, m, testProjectDir(t), time.Minute)
		p, _ := m.Store().GetPRD(prd.ID)
		m.Store().WithLock(func() { p.MergedInRelease, p.MergeCommit = "v1.0.0", "abcdef1234567" })
		closeRecord(t, m, prd.ID, PRDApproved, OutcomeSuperseded)
		if got := m.DeliverShippedQueued(); len(got) != 0 {
			t.Errorf("a closed record was changed by the shipped queue drop: %v", got)
		}
	})
	t.Run("stuck running watchdog", func(t *testing.T) {
		m, api := gateFixture(t)
		m.SetSessionListerFn(func() []SessionInfo { return nil })
		now := time.Now()
		m.orphanNow = func() time.Time { return now }
		prd := gatePRD(t, m, testProjectDir(t), time.Minute)
		p, _ := m.Store().GetPRD(prd.ID)
		m.Store().WithLock(func() { p.Story[0].Tasks[0].Status = TaskCompleted })
		closeRecord(t, m, prd.ID, PRDRunning, OutcomeDelivered)
		var refired int
		m.SetStuckRunningHooks(func(string) (bool, error) { refired++; return false, nil }, api.executorLive)
		for i := 0; i < 4; i++ {
			m.sweepStuckRunning()
			now = now.Add(24 * time.Hour)
		}
		if got, _ := m.Store().SnapshotPRD(prd.ID); refired != 0 || got.Status != PRDRunning {
			t.Errorf("refired %d time(s), status %q; the watchdog must leave a closed record alone", refired, got.Status)
		}
	})
}

// An answered question of a closed record applies nothing and says so once.
func TestAnswerOnClosedRecordIsNotApplied(t *testing.T) {
	r := newProtectedAnswerRig(t, []string{"AGENT.md"}, "AGENT.md")
	api := NewAPI(r.m)
	var changes []QuestionChange
	r.store.SetChangeHook(func(c QuestionChange) {
		changes = append(changes, c)
		api.OnQuestionChange(c)
	})
	closeRecord(t, r.m, r.prd.ID, PRDCancelled, OutcomeSuperseded)

	r.answer(t, ProtectedPathsOptionApprove)
	if len(changes) == 0 {
		t.Fatal("the answer raised no change")
	}
	api.OnQuestionChange(changes[len(changes)-1]) // delivered twice

	prd, tk := r.reload(t)
	if prd.Status != PRDCancelled || tk.Status != TaskBlocked || tk.ProtectedPathHold == nil {
		t.Errorf("status %q task %q hold %+v; an answer must not change a closed record", prd.Status, tk.Status, tk.ProtectedPathHold)
	}
	if n := decisionCount(t, r.m, r.prd.ID, "closed_record_answer_ignored"); n != 1 {
		t.Errorf("answer-ignored decisions = %d; want 1", n)
	}
}

// An archive with a recorded outcome is not unarchived by the one-time repair.
func TestUnarchiveRepairSkipsRecordedOutcome(t *testing.T) {
	m, _ := gateFixture(t)
	prd := gatePRD(t, m, testProjectDir(t), time.Minute)
	p, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() {
		p.Git.Branch = "automaton/x"
		p.Decisions = append(p.Decisions, Decision{At: time.Now(), Kind: "archive", Actor: "supervisor-loop"})
	})
	closeRecord(t, m, prd.ID, PRDArchived, OutcomeSuperseded)
	ops := ManagerUnarchiveOps{M: m}
	if got := ops.Candidates(context.Background()); len(got) != 0 {
		t.Errorf("candidates = %+v; a recorded outcome stays archived", got)
	}
	if err := ops.Restore(context.Background(), UnarchiveCandidate{ID: prd.ID, RestoreTo: PRDCompleted}); err == nil {
		t.Error("the repair restored an archive with a recorded outcome")
	}
}

// Resume does not start a record with a closed outcome, and the operator's
// force past the shipped-work rule clears the outcome it overrides.
func TestResumeRefusedAndForceClearsOutcome(t *testing.T) {
	m, _ := gateFixture(t)
	prd := gatePRD(t, m, testProjectDir(t), time.Minute)
	closeRecord(t, m, prd.ID, PRDPaused, OutcomeSuperseded)
	if _, err := m.Resume(prd.ID, "operator"); err == nil {
		t.Error("a paused record with a closed outcome was resumed")
	}
	p, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() {
		p.MergedInRelease, p.MergeCommit = "v1.0.0", "abcdef1234567"
		p.Status = PRDCompleted
		p.ShippedRefusal = &ShippedRefusalRecord{Release: "v1.0.0", Commit: "abcdef1234567", From: PRDApproved}
	})
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	got, err := m.ForceClosedWork(prd.ID, "operator", "run it again")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != PRDApproved || got.ClosedOutcome != "" {
		t.Errorf("status = %q outcome = %q; want approved with the outcome cleared", got.Status, got.ClosedOutcome)
	}
}
