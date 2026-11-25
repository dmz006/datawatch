// The operator pause holds the work the engine starts by itself: merge
// passes, child and scan-fix planning, planner retries. An operator-started
// plan skips the usage back-off, a planner the operator ended is not started
// again, and a task attempt records its queue wait and verify time.

package autonomous

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/pause"
)

const followupPlan = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`

func pauseAll(t *testing.T, ps *pause.Store) {
	t.Helper()
	if _, err := ps.Pause(pause.ScopeGlobal, "", "freeze", "operator"); err != nil {
		t.Fatal(err)
	}
}

func resumeAll(t *testing.T, ps *pause.Store) {
	t.Helper()
	if _, err := ps.Resume(pause.ScopeGlobal, "", "operator", ""); err != nil {
		t.Fatal(err)
	}
}

func pausedManager(t *testing.T, m *Manager) *pause.Store {
	t.Helper()
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	m.SetPauseWaitPoll(2 * time.Millisecond)
	return ps
}

func TestTaskMergeStallPass_PausedProjectIsNotMerged(t *testing.T) {
	h := newStallHarness(t, "on")
	ps := pausedManager(t, h.m)
	pauseAll(t, ps)
	h.poll()
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	if n := h.merger.count(); n != 0 {
		t.Fatalf("the stalled-merge retry ran %d time(s) while paused", n)
	}
	got := h.stallAlerts()
	if len(got) != 1 || !strings.Contains(got[0].body, "paused by operator") {
		t.Fatalf("the one stall alert must say the retry is paused: %+v", got)
	}
	// The pause did not consume the retry slot: the first poll after the
	// resume retries, well inside taskMergeStallRetryEvery.
	resumeAll(t, ps)
	h.now = h.now.Add(time.Minute)
	h.poll()
	if n := h.merger.count(); n != 1 {
		t.Fatalf("retries after the resume = %d, want 1", n)
	}
}

func TestCompletionMergePass_PausedFoldsOnlyTheFinishedTask(t *testing.T) {
	for _, paused := range []bool{true, false} {
		f := newMergeFixture(t, "", nil)
		f.addTask("finished", nil, "a.txt", "a\n")
		f.addTask("older", nil, "b.txt", "b\n")
		f.seal()
		ps := pausedManager(t, f.m)
		if paused {
			pauseAll(t, ps)
		}
		f.m.foldFinishedTask(context.Background(), f.prd, f.task("finished"))
		if got := f.task("finished").MergeState; got != MergeStateMerged {
			t.Fatalf("paused=%v: the finished task must be folded, state %q", paused, got)
		}
		merged := f.task("older").MergeState == MergeStateMerged
		if merged == paused {
			t.Fatalf("paused=%v: the other waiting task merged=%v", paused, merged)
		}
	}
}

// childFixture is a running parent whose one task spawns a child Automaton.
func childFixture(t *testing.T) (*Manager, *PRD, *Task) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	m.mu.Lock()
	m.cfg.AutoApproveChildren = true
	m.cfg.MaxRecursionDepth = 3
	m.mu.Unlock()
	parent, err := m.CreatePRD("parent", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Store().SetStories(parent.ID, []Story{{Title: "S", Tasks: []Task{{Title: "Spawn child", Spec: "child spec", SpawnPRD: true}}}}); err != nil {
		t.Fatal(err)
	}
	parent, _ = m.Store().GetPRD(parent.ID)
	m.store.WithLock(func() { parent.Status = PRDRunning })
	return m, parent, &parent.Story[0].Tasks[0]
}

func quietSpawn(spawns *atomic.Int32) SpawnFn {
	return func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		spawns.Add(1)
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
}

func quietVerify(context.Context, *PRD, *Task) (VerificationResult, error) {
	return VerificationResult{OK: true}, nil
}

func TestChildPlanning_WaitsWhilePaused(t *testing.T) {
	m, parent, task := childFixture(t)
	ps := pausedManager(t, m)
	pauseAll(t, ps)
	var plans, spawns atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) { plans.Add(1); return followupPlan, nil }

	done := make(chan error, 1)
	go func() {
		done <- m.recurseChildPRD(context.Background(), parent, task, quietSpawn(&spawns), quietVerify, 1)
	}()
	time.Sleep(80 * time.Millisecond)
	if plans.Load() != 0 || spawns.Load() != 0 {
		t.Fatalf("while paused: %d planner call(s), %d spawn(s)", plans.Load(), spawns.Load())
	}
	snap := taskSnapshot(t, m, task.ID)
	if snap.Status != TaskWaitingCapacity || !strings.Contains(snap.WaitReason, "paused by operator") {
		t.Fatalf("the parent task must wait with the pause reason, got %q %q", snap.Status, snap.WaitReason)
	}
	resumeAll(t, ps)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child not planned after the resume")
	}
	if got := taskSnapshot(t, m, task.ID); got.Status != TaskCompleted || plans.Load() != 1 {
		t.Fatalf("after the resume: status %q error %q plans %d", got.Status, got.Error, plans.Load())
	}
}

func TestChildRun_WaitsWhenPausedDuringChildPlanning(t *testing.T) {
	m, parent, task := childFixture(t)
	ps := pausedManager(t, m)
	var spawns atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) {
		pauseAll(t, ps)
		return followupPlan, nil
	}
	done := make(chan error, 1)
	go func() {
		done <- m.recurseChildPRD(context.Background(), parent, task, quietSpawn(&spawns), quietVerify, 1)
	}()
	time.Sleep(150 * time.Millisecond)
	if got := taskSnapshot(t, m, task.ID); got.Status == TaskFailed || spawns.Load() != 0 {
		t.Fatalf("a pause during child planning must make the run wait, got %q %q spawns %d", got.Status, got.Error, spawns.Load())
	}
	resumeAll(t, ps)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child not run after the resume")
	}
	if got := taskSnapshot(t, m, task.ID); got.Status != TaskCompleted {
		t.Fatalf("after the resume: status %q error %q", got.Status, got.Error)
	}
}

func TestScanFixPlanning_WaitsWhilePaused(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	parent, err := m.CreatePRD("parent", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	// The plan coverage gate is not under test here.
	m.mu.Lock()
	m.cfg.PlanCoverageCheck = "off"
	m.mu.Unlock()
	ps := pausedManager(t, m)
	pauseAll(t, ps)
	var plans atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) { plans.Add(1); return followupPlan, nil }
	before := len(m.Store().ListPRDs())

	type res struct {
		id  string
		err error
	}
	done := make(chan res, 1)
	go func() {
		id, err := m.fixLoopDeps().OpenFix(parent.ID, []scan.Finding{{RuleID: "r1", Severity: scan.SeverityError, Message: "broken"}})
		done <- res{id, err}
	}()
	time.Sleep(80 * time.Millisecond)
	if plans.Load() != 0 || len(m.Store().ListPRDs()) != before {
		t.Fatalf("while paused: %d planner call(s), %d new Automaton(s)", plans.Load(), len(m.Store().ListPRDs())-before)
	}
	resumeAll(t, ps)
	select {
	case r := <-done:
		if r.err != nil || r.id == "" || plans.Load() != 1 {
			t.Fatalf("after the resume: id %q err %v plans %d", r.id, r.err, plans.Load())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("fix task not planned after the resume")
	}
}

func TestEnginePlanner_RetryWaitsWhilePaused(t *testing.T) {
	m, _, prd, _ := planGateFixture(t)
	ps := pausedManager(t, m)
	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) {
		if calls.Add(1) == 1 {
			pauseAll(t, ps)
			return "", &OneShotNoOutputError{Msg: "no output"}
		}
		return followupPlan, nil
	}
	done := make(chan error, 1)
	go func() { _, err := m.DecomposeAutomatic(prd.ID); done <- err }()
	time.Sleep(80 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("an engine-started planner was started again while paused: %d calls", n)
	}
	resumeAll(t, ps)
	select {
	case err := <-done:
		if err != nil || calls.Load() != 2 {
			t.Fatalf("after the resume: err %v calls %d", err, calls.Load())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("planner not retried after the resume")
	}
}

func TestEnginePlanner_QueuedPlannerIsNotStartedWhilePaused(t *testing.T) {
	m, bo, clk, led := backoffFixture(t)
	ps := pausedManager(t, m)
	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
	m.mu.Lock()
	m.cfg.PlanningBackend = "claude-code"
	m.mu.Unlock()
	bo.Set("claude-code", clk.Now().Add(30*time.Second), "rate-limit", BackoffSourceRateLimit)
	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) { calls.Add(1); return followupPlan, nil }
	done := make(chan error, 1)
	go func() { _, err := m.DecomposeAutomatic(prd.ID); done <- err }()
	pollUntil(t, 2*time.Second, "planner queued", func() bool { return plannerQueued(led, prd.ID) })
	pauseAll(t, ps)
	clk.waitTimers(t, 1)
	clk.Advance(31 * time.Second) // the back-off ends while the project is paused
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("a queued planner started while paused")
	}
	resumeAll(t, ps)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-done:
			if err != nil || calls.Load() != 1 {
				t.Fatalf("after the resume: err %v calls %d", err, calls.Load())
			}
			return
		case <-deadline:
			t.Fatal("planner not started after the resume")
		case <-time.After(5 * time.Millisecond):
			clk.Advance(time.Second)
		}
	}
}

func TestDecomposeAdmitted_OperatorStartedPlanSkipsBackoff(t *testing.T) {
	m, bo, clk, led := backoffFixture(t)
	api := NewAPI(m)
	drainOnCleanup(t, m, api)
	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
	m.mu.Lock()
	m.cfg.PlanningBackend = "claude-code"
	m.mu.Unlock()
	bo.Set("claude-code", clk.Now().Add(time.Hour), "rate-limit", BackoffSourceRateLimit)
	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) { calls.Add(1); return followupPlan, nil }
	done := make(chan error, 1)
	go func() { _, err := api.Decompose(prd.ID); done <- err }()
	select {
	case err := <-done:
		if err != nil || calls.Load() != 1 {
			t.Fatalf("operator plan: err %v calls %d", err, calls.Load())
		}
	case <-time.After(3 * time.Second):
		reason := prdWaitReason(t, m, prd.ID)
		clk.Advance(2 * time.Hour)
		if err := <-done; err != nil {
			t.Log(err)
		}
		t.Fatalf("an operator-started plan was held by the usage back-off: %q (queued %v)", reason, plannerQueued(led, prd.ID))
	}
}

// A queued operator plan the operator moves to another pool stays exempt: the
// retargeted queue entry must not lose the flag.
func TestDecomposeAdmitted_RetargetedOperatorPlanStaysExempt(t *testing.T) {
	m, bo, clk, led := backoffFixture(t)
	api := NewAPI(m)
	drainOnCleanup(t, m, api)
	led.SetLimit("llm:pa", 1)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "busy:pa", Pools: []string{"llm:pa"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.CreatePRD("s", t.TempDir(), "pa", "oldmodel", EffortNormal)
	seedPlanningProfile(t, m, prd.ID, "pa", "oldmodel")
	bo.Set("pb", clk.Now().Add(time.Hour), "rate-limit", BackoffSourceRateLimit)
	m.decompose = func(DecomposeRequest) (string, error) { return followupPlan, nil }
	done := make(chan error, 1)
	go func() { _, err := api.Decompose(prd.ID); done <- err }()
	pollUntil(t, 3*time.Second, "planner queued on the full pool", func() bool { return plannerQueued(led, prd.ID) })
	pb, model := "pb", "newmodel"
	if _, res, err := m.SetPRDLLM(prd.ID, nil, nil, nil, &pb, &model, "operator"); err != nil || res == nil || res.Outcome != PlanningRequeued {
		t.Fatalf("retarget: res %+v err %v", res, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		reason := prdWaitReason(t, m, prd.ID)
		clk.Advance(2 * time.Hour)
		if err := <-done; err != nil {
			t.Log(err)
		}
		t.Fatalf("the retargeted operator plan was held by the back-off: %q", reason)
	}
}

// The control: a plan the engine starts is still held by the back-off.
func TestDecomposeAdmitted_AutomaticPlanStillWaitsOnBackoff(t *testing.T) {
	m, bo, clk, led := backoffFixture(t)
	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
	m.mu.Lock()
	m.cfg.PlanningBackend = "claude-code"
	m.mu.Unlock()
	bo.Set("claude-code", clk.Now().Add(time.Hour), "rate-limit", BackoffSourceRateLimit)
	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) { calls.Add(1); return followupPlan, nil }
	done := make(chan error, 1)
	go func() { _, err := m.DecomposeAutomatic(prd.ID); done <- err }()
	pollUntil(t, 2*time.Second, "planner waiting on back-off", func() bool {
		return plannerQueued(led, prd.ID) && strings.Contains(prdWaitReason(t, m, prd.ID), "backoff:")
	})
	if calls.Load() != 0 {
		t.Fatalf("an automatic plan ran during the back-off")
	}
	clk.waitTimers(t, 1)
	clk.Advance(2 * time.Hour)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("planner not admitted after the back-off")
	}
}

func operatorEndedErr() error {
	return &OneShotNoOutputError{Msg: "one-shot decompose session ended with state killed without producing output", OperatorEndedBy: "command.kill"}
}

func TestNoteDecomposeSessionFailure_OperatorEndedPlannerIsNotRetried(t *testing.T) {
	m, _, prd, calls := planGateFixture(t)
	m.decompose = func(DecomposeRequest) (string, error) {
		calls.Add(1)
		return "", operatorEndedErr()
	}
	if _, err := m.Decompose(prd.ID); err == nil {
		t.Fatal("want the planner error")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("an operator-ended planner was started again: %d calls", n)
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	fr := cur.LastDecomposeFailure
	if cur.Status != PRDFailed || fr == nil || fr.Retryable || !strings.Contains(fr.Detail, "ended by the operator (command.kill)") {
		t.Fatalf("status %q failure %+v, want it left for the operator", cur.Status, fr)
	}
	if cur.DecomposeSessionFailures != 0 {
		t.Fatalf("an operator action counted as a session failure: %d", cur.DecomposeSessionFailures)
	}
	// The engine does not plan it again by itself.
	if _, err := m.DecomposeAutomatic(prd.ID); err == nil || calls.Load() != 1 {
		t.Fatalf("the engine planned it again: err %v calls %d", err, calls.Load())
	}
}

func TestFailPlanningReattach_OperatorEndedPlannerStaysStopped(t *testing.T) {
	m, _, prd, _ := planGateFixture(t)
	m.store.MutatePRD(prd.ID, func(p *PRD) bool { p.Status = PRDPlanning; return true })
	m.FailPlanningReattach(prd.ID, operatorEndedErr())
	cur, _ := m.Store().GetPRD(prd.ID)
	if cur.Status != PRDFailed {
		t.Fatalf("status %q, want failed (a draft would be planned again by lane fill)", cur.Status)
	}
}

func TestOperatorEndCaller(t *testing.T) {
	for note, want := range map[string]string{
		"killed by cli.session-kill: kill CLI":              "cli.session-kill",
		"killed by command.kill: from web":                  "command.kill",
		"killed by rest.session-kill: REST POST":            "rest.session-kill",
		"killed by comm.kill: comm channel kill":            "comm.kill",
		"killed by mcp.kill-session: MCP":                   "mcp.kill-session",
		"killed by daemon.shutdown: signal":                 "",
		"killed by autonomous.verifier.idle-detector: idle": "",
		"process gone, finalised":                           "",
		"complete; closed by command.kill: x":               "",
		"":                                                  "",
	} {
		if got := OperatorEndCaller(note); got != want {
			t.Errorf("OperatorEndCaller(%q) = %q, want %q", note, got, want)
		}
	}
}

func TestTaskAttempt_RecordsQueueWaitAndVerifySeconds(t *testing.T) {
	m, bo, clk, _ := backoffFixture(t)
	m.attemptClock = clk.Now
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	taskID := cur.Story[0].Tasks[0].ID
	m.store.WithLock(func() { cur.Status = PRDApproved; cur.Backend = "claude-code" })
	bo.Set("claude-code", clk.Now().Add(39*time.Second), "rate-limit", BackoffSourceRateLimit)
	var spawns, verifies atomic.Int32
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		if verifies.Add(1) == 1 {
			clk.Advance(90 * time.Second)
			return VerificationResult{OK: false, Summary: "not done"}, nil
		}
		return VerificationResult{OK: true}, nil
	}
	done := make(chan error, 1)
	go func() { done <- m.Run(context.Background(), prd.ID, quietSpawn(&spawns), verify) }()
	pollUntil(t, 2*time.Second, "task waiting for its lease", func() bool {
		return taskSnapshot(t, m, taskID).Status == TaskWaitingCapacity
	})
	clk.waitTimers(t, 1)
	clk.Advance(40 * time.Second)
	deadline := time.After(15 * time.Second)
	for running := true; running; {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			running = false
		case <-deadline:
			t.Fatalf("run did not end (verifies %d)", verifies.Load())
		case <-time.After(20 * time.Millisecond):
			if verifies.Load() > 0 {
				clk.Advance(time.Second)
			}
		}
	}
	got := taskSnapshot(t, m, taskID)
	if len(got.Attempts) != 1 {
		t.Fatalf("attempts = %d (%+v)", len(got.Attempts), got.Attempts)
	}
	a := got.Attempts[0]
	if a.QueueWaitSeconds != 40 || a.VerifySeconds != 90 {
		t.Fatalf("queue_wait_seconds=%d verify_seconds=%d, want 40 and 90", a.QueueWaitSeconds, a.VerifySeconds)
	}
}

// A verifier that waits for the worker first reports when verification really
// starts; the verify time then leaves the worker's run out.
func TestTaskAttempt_VerifierReportsWhenVerificationStarts(t *testing.T) {
	m, _, clk, _ := backoffFixture(t)
	m.attemptClock = clk.Now
	m.NoteVerificationStarted("t1") // not in verification: nothing to move
	if _, v := m.attemptSeconds("t1"); v != 0 {
		t.Fatalf("verify = %d with no verification", v)
	}
	m.noteVerifyStart("t1")
	clk.Advance(500 * time.Second) // the worker runs
	m.NoteVerificationStarted("t1")
	clk.Advance(7 * time.Second)
	m.noteVerdict("t1")
	if _, v := m.attemptSeconds("t1"); v != 7 {
		t.Fatalf("verify = %d, want 7", v)
	}
	m.resetAttemptTiming("t1")
	if q, v := m.attemptSeconds("t1"); q != 0 || v != 0 {
		t.Fatalf("after the reset: %d %d", q, v)
	}
}
