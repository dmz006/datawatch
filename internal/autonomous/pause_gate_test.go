package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/pause"
)

func newPauseStore(t *testing.T) *pause.Store {
	t.Helper()
	s, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPauseGate_NoGateChangesNothing(t *testing.T) {
	m, prd := bl386Setup(t)
	var spawns atomic.Int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawns.Add(1)
		return SpawnResult{SessionID: "s"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run with no pause gate: %v", err)
	}
	if spawns.Load() == 0 {
		t.Fatal("no gate wired: the task must start as before")
	}
}

func TestPauseGate_RunRefusedWhenGloballyPaused(t *testing.T) {
	m, prd := bl386Setup(t)
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeGlobal, "", "release freeze", "operator"); err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawns.Add(1)
		return SpawnResult{SessionID: "s"}, nil
	}
	err := m.Run(context.Background(), prd.ID, spawn, nil)
	if err == nil || !strings.Contains(err.Error(), "paused by operator") || !strings.Contains(err.Error(), "release freeze") {
		t.Fatalf("Run err = %v, want a paused-by-operator refusal naming the reason", err)
	}
	if spawns.Load() != 0 {
		t.Fatalf("a paused Automaton spawned %d session(s)", spawns.Load())
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	if cur.Status != PRDApproved {
		t.Fatalf("a refused start must leave the status alone, got %q", cur.Status)
	}
}

func TestPauseGate_ProjectPauseIsScoped(t *testing.T) {
	m, prd := bl386Setup(t) // project /proj
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeProject, "/other", "elsewhere", "operator"); err != nil {
		t.Fatal(err)
	}
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) { return SpawnResult{SessionID: "s"}, nil }
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("a pause on another project must not block this one: %v", err)
	}

	m2, prd2 := bl386Setup(t)
	ps2 := newPauseStore(t)
	m2.SetPauseGate(ps2)
	_, _ = ps2.Pause(pause.ScopeProject, "/proj", "this one", "operator")
	if err := m2.Run(context.Background(), prd2.ID, spawn, nil); err == nil || !strings.Contains(err.Error(), "project /proj") {
		t.Fatalf("a pause on this project must refuse the start, err = %v", err)
	}
}

func TestPauseGate_ApiRunAndBootResumeRefuseAndPark(t *testing.T) {
	m, prd := bl386Setup(t)
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	a := NewAPI(m)
	if err := a.Run(prd.ID); err == nil || !strings.Contains(err.Error(), "paused by operator") {
		t.Fatalf("API.Run err = %v, want a pause refusal", err)
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	if cur.Status != PRDApproved {
		t.Fatalf("status after a refused API.Run = %q", cur.Status)
	}
}

func TestPauseGate_WorkerAdmissionWaitsWithReasonAndResumes(t *testing.T) {
	f := newAdmitFixture(t, nil)
	ps := newPauseStore(t)
	f.m.SetPauseGate(ps)
	f.m.SetPauseWaitPoll(5 * time.Millisecond)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")

	done := f.admitAsync(context.Background(), capacity.RoleWorker)
	pollUntil(t, 3*time.Second, "task waiting on the pause", func() bool {
		return strings.Contains(f.taskNow().WaitReason, "paused by operator")
	})
	if cur := f.taskNow(); cur.Status != TaskWaitingCapacity || cur.RetryCount != 0 {
		t.Fatalf("a paused task waits, it does not fail or retry: %+v", cur)
	}
	select {
	case err := <-done:
		t.Fatalf("admit returned %v while paused", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := ps.Resume(pause.ScopeGlobal, "", "operator", ""); err != nil {
		t.Fatal(err)
	}
	expectAdmitted(t, done)
	if cur := f.taskNow(); cur.WaitReason != "" {
		t.Fatalf("wait reason must clear after the resume: %q", cur.WaitReason)
	}
}

func TestPauseGate_OtherRolesAreNotHeld(t *testing.T) {
	f := newAdmitFixture(t, nil)
	ps := newPauseStore(t)
	f.m.SetPauseGate(ps)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	// A verifier is part of work already running: the pause must not hold it.
	done := f.admitAsync(context.Background(), capacity.RoleVerifier)
	expectAdmitted(t, done)
}

func TestPauseGate_CancelledTaskLeavesTheWait(t *testing.T) {
	f := newAdmitFixture(t, nil)
	ps := newPauseStore(t)
	f.m.SetPauseGate(ps)
	f.m.SetPauseWaitPoll(5 * time.Millisecond)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	ctx, cancel := context.WithCancel(context.Background())
	done := f.admitAsync(ctx, capacity.RoleWorker)
	pollUntil(t, 3*time.Second, "waiting", func() bool { return strings.Contains(f.taskNow().WaitReason, "paused") })
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled wait must return an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the wait did not end on cancel")
	}
}

func TestPauseGate_RunningWorkFinishesNewTasksWait(t *testing.T) {
	m, prd := bl386Setup(t)
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	m.SetPauseWaitPoll(5 * time.Millisecond)
	// Two tasks in the story; the second depends on nothing but runs after
	// the first (one task slot), so it is a new admission after the pause.
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "first", Spec: "do one"}, {Title: "second", Spec: "do two"},
	}}}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var started []string
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		started = append(started, r.Title)
		n := len(started)
		mu.Unlock()
		if n == 1 {
			// The operator pauses while the first task is running.
			_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
		}
		return SpawnResult{SessionID: "s"}, nil
	}
	errc := make(chan error, 1)
	go func() { errc <- m.Run(context.Background(), prd.ID, spawn, nil) }()
	pollUntil(t, 5*time.Second, "second task waiting on the pause", func() bool {
		snap, _ := m.Store().SnapshotPRD(prd.ID)
		for _, tk := range snap.Story[0].Tasks {
			if tk.Title == "second" && strings.Contains(tk.WaitReason, "paused by operator") {
				return true
			}
		}
		return false
	})
	mu.Lock()
	if len(started) != 1 || started[0] != "first" {
		mu.Unlock()
		t.Fatalf("started = %v, want only the first task while paused", started)
	}
	mu.Unlock()
	_, _ = ps.Resume(pause.ScopeGlobal, "", "operator", "")
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run after resume: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not finish after the resume")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(started) != 2 {
		t.Fatalf("started = %v, want both tasks after the resume", started)
	}
}

func TestPauseGate_LaneFillRefusesPausedCandidate(t *testing.T) {
	c := LaneFillCandidate{ID: "p1", Status: PRDApproved, PausedReason: "paused by operator (all work): freeze"}
	code, text := EvaluateLaneFillEligibility(LaneFillConfig{}, c, time.Now())
	if code != LaneFillReasonOperatorPause || !strings.Contains(text, "freeze") {
		t.Fatalf("refusal = %q %q, want operator_pause naming the reason", code, text)
	}
	c.PausedReason = ""
	if code, _ := EvaluateLaneFillEligibility(LaneFillConfig{}, c, time.Now()); code == LaneFillReasonOperatorPause {
		t.Fatal("an unpaused candidate must not be refused for a pause")
	}
}

func TestPauseGate_RunningAutomatonRelaunchStartsNothingWhilePaused(t *testing.T) {
	m, prd := bl386Setup(t)
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	m.SetPauseWaitPoll(5 * time.Millisecond)
	// A daemon restart leaves the Automaton running with no executor: the
	// relaunch must adopt it, but start no new worker until the resume.
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	_ = m.Store().SavePRD(cur)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	var spawns atomic.Int32
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawns.Add(1)
		return SpawnResult{SessionID: "s"}, nil
	}
	errc := make(chan error, 1)
	go func() { errc <- m.Run(context.Background(), prd.ID, spawn, nil) }()
	pollUntil(t, 5*time.Second, "task waiting on the pause", func() bool {
		snap, _ := m.Store().SnapshotPRD(prd.ID)
		return strings.Contains(snap.Story[0].Tasks[0].WaitReason, "paused by operator")
	})
	if n := spawns.Load(); n != 0 {
		t.Fatalf("%d worker(s) started while paused", n)
	}
	_, _ = ps.Resume(pause.ScopeGlobal, "", "operator", "")
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run after resume: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not finish after the resume")
	}
	if spawns.Load() != 1 {
		t.Fatalf("spawns = %d, want 1 after the resume", spawns.Load())
	}
}

func TestPauseGate_PausedReasonForFeedsLaneFill(t *testing.T) {
	m, _ := bl386Setup(t)
	if got := m.pausedReasonFor("/proj"); got != "" {
		t.Fatalf("no gate: reason = %q", got)
	}
	ps := newPauseStore(t)
	m.SetPauseGate(ps)
	_, _ = ps.Pause(pause.ScopeProject, "/proj", "review", "operator")
	if got := m.pausedReasonFor("/proj"); !strings.Contains(got, "review") {
		t.Fatalf("paused project reason = %q", got)
	}
	if got := m.pausedReasonFor("/else"); got != "" {
		t.Fatalf("another project reason = %q", got)
	}
}
