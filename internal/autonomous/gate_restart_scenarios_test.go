//go:build unix

package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Scenario tests for the final gate across a daemon restart. They use a temp
// project, a short fake gate script that waits for a release file, a fake
// clock and no real daemon or service manager. Nothing here touches a
// session, sleeps for real minutes or spins a CPU loop: the gate script
// sleeps between file checks and the tests poll in tens of milliseconds.

// gateScenario is one project with a release-gated gate script and the data
// directory a "restarted" Manager is rebuilt over.
type gateScenario struct {
	dataDir    string
	projectDir string
	release    string
	count      string
	prdID      string
	taskID     string
	cfg        Config
}

// newGateScenario sets a temp run root (shared by every Manager built over
// this scenario) and creates an Automaton whose final gate waits for the
// release file. drainMinutes is autonomous.gate_drain_minutes.
func newGateScenario(t *testing.T, drainMinutes int) (*gateScenario, *Manager) {
	t.Helper()
	prevRoot, prevPoll := verificationRunRoot(), gateWaitPollInterval
	SetVerificationRunRoot(filepath.Join(t.TempDir(), "runs"))
	gateWaitPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { SetVerificationRunRoot(prevRoot); gateWaitPollInterval = prevPoll })

	s := &gateScenario{dataDir: t.TempDir(), projectDir: t.TempDir()}
	s.release = filepath.Join(s.projectDir, "release")
	s.count = filepath.Join(s.projectDir, "runs.count")
	body := "echo run >> " + s.count + "\n" +
		"while [ ! -f " + s.release + " ]; do sleep 0.1; done\n" +
		"echo gate-done\nexit 0\n"
	if err := os.MkdirAll(filepath.Join(s.projectDir, gateVerifyFolder), 0o755); err != nil {
		t.Fatal(err)
	}
	script := gateVerifyFolder + "/gate.sh"
	if err := os.WriteFile(filepath.Join(s.projectDir, script), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	s.cfg = DefaultConfig()
	s.cfg.ScriptGateTimeoutSeconds = 60
	s.cfg.GateDrainMinutes = drainMinutes

	m := s.manager(t)
	prd, err := m.CreatePRD("spec", "", "pi-prompt", "qwen3", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	gate := Task{Title: "gate", Spec: "run the gate", Kind: TaskKindFinalGate, VerificationScript: script}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "worker", Spec: "do work", Status: TaskCompleted}, gate,
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	prd.ProjectDir = s.projectDir
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	s.prdID, s.taskID = prd.ID, prd.Story[0].Tasks[1].ID
	t.Cleanup(s.releaseGate) // a gate left waiting always ends with the test
	return s, m
}

// manager builds a Manager over the scenario's data directory: the first one
// is the daemon, a later one is the daemon after a restart.
func (s *gateScenario) manager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(s.dataDir, s.cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func (s *gateScenario) load(t *testing.T, m *Manager) (*PRD, *Task) {
	t.Helper()
	prd, ok := m.Store().GetPRD(s.prdID)
	if !ok {
		t.Fatalf("PRD %s not found", s.prdID)
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == s.taskID {
				return prd, &prd.Story[si].Tasks[ti]
			}
		}
	}
	t.Fatalf("task %s not found", s.taskID)
	return nil, nil
}

func (s *gateScenario) check() GateCheck {
	return GateCheck{Kind: GateCheckScript, Script: gateVerifyFolder + "/gate.sh"}
}

func (s *gateScenario) runDir() string {
	return gateRunDirFor(verificationRunRoot(), s.check(), s.projectDir)
}

func (s *gateScenario) releaseGate() { _ = os.WriteFile(s.release, []byte("go\n"), 0o600) }

func (s *gateScenario) runs(t *testing.T) int {
	t.Helper()
	b, _ := os.ReadFile(s.count)
	return strings.Count(string(b), "run")
}

// waitUntil polls cond every 10ms for up to 15s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type gateRunOutcome struct {
	handled bool
	err     error
}

// runningGate is a gate task running under one Manager in a goroutine.
type runningGate struct {
	cancel context.CancelFunc
	done   chan gateRunOutcome
}

// startGate runs the gate task under m through the durable path and returns
// once the gate process is up, recorded and registered. The process and the
// goroutine are cleaned up with the test.
func (s *gateScenario) startGate(t *testing.T, m *Manager) runningGate {
	t.Helper()
	prd, task := s.load(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	g := runningGate{cancel: cancel, done: make(chan gateRunOutcome, 1)}
	go func() {
		handled, err := m.runScriptGateTask(ctx, prd, task)
		g.done <- gateRunOutcome{handled, err}
	}()
	t.Cleanup(func() {
		s.releaseGate()
		cancel()
		select {
		case <-g.done:
		case <-time.After(15 * time.Second):
		}
		if rec, err := readGateRecord(s.runDir()); err == nil && rec.PID > 0 {
			killGateGroup(rec.PID)
		}
	})
	waitUntil(t, "the gate process to be recorded and registered", func() bool {
		rec, err := readGateRecord(s.runDir())
		gates := m.RunningGates()
		return err == nil && rec.PID > 0 && len(gates) == 1 && gates[0].PID == rec.PID
	})
	return g
}

func (g runningGate) wait(t *testing.T) gateRunOutcome {
	t.Helper()
	select {
	case o := <-g.done:
		g.done <- o // keep it readable so the cleanup does not wait again
		return o
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the gate task to return")
		return gateRunOutcome{}
	}
}

// markRunningAndStop is the first daemon dying: the gate task is left
// in_progress on disk, the daemon's context ends (the gate keeps running).
func (s *gateScenario) markRunningAndStop(t *testing.T, m *Manager, g runningGate) {
	t.Helper()
	_, task := s.load(t, m)
	m.store.WithLock(func() { task.Status = TaskInProgress })
	if err := m.store.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	g.cancel()
	if o := g.wait(t); !o.handled {
		t.Fatalf("first daemon: gate task not handled: %+v", o)
	}
}

func TestGateSurvivesRestart(t *testing.T) {
	t.Run("still running at restart", func(t *testing.T) {
		s, m1 := newGateScenario(t, 0)
		g := s.startGate(t, m1)
		recBefore, err := readGateRecord(s.runDir())
		if err != nil || recBefore.PID <= 0 || recBefore.ProcStart.IsZero() || recBefore.Delivered {
			t.Fatalf("durable record before restart: %+v err %v", recBefore, err)
		}
		s.markRunningAndStop(t, m1, g)
		if !gateRecordAlive(recBefore) {
			t.Fatal("the gate must keep running after the first daemon stops")
		}

		// The restart: all in-memory state is gone; a fresh Manager reads the
		// same data directory and run root.
		m2 := s.manager(t)
		prd2, task2 := s.load(t, m2)
		m2.resetInProgressTasksForResume(prd2)
		if task2.Status != TaskPending || task2.RetryCount != 0 || task2.Error != "" {
			t.Fatalf("boot resume must hold the running gate: status=%q retries=%d err=%q", task2.Status, task2.RetryCount, task2.Error)
		}

		g2 := s.startGate(t, m2)
		gates := m2.RunningGates()
		if len(gates) != 1 || gates[0].PID != recBefore.PID {
			t.Fatalf("new Manager must re-attach to pid %d, got %+v", recBefore.PID, gates)
		}
		recAfter, _ := readGateRecord(s.runDir())
		if recAfter.PID != recBefore.PID || !recAfter.ProcStart.Equal(recBefore.ProcStart) {
			t.Fatalf("record changed on re-attach: before %+v after %+v", recBefore, recAfter)
		}

		s.releaseGate()
		if o := g2.wait(t); !o.handled || o.err != nil {
			t.Fatalf("re-attached gate: %+v", o)
		}
		got, _ := reloadedTask(t, m2, s.prdID, s.taskID)
		if got.Status != TaskCompleted || got.RetryCount != 0 {
			t.Fatalf("verdict must come from the released gate's result: status=%q retries=%d", got.Status, got.RetryCount)
		}
		if n := s.runs(t); n != 1 {
			t.Fatalf("gate ran %d times, want 1 (re-attached, never restarted)", n)
		}
		if rep, _ := os.ReadFile(filepath.Join(s.projectDir, defaultGateReportPath)); !strings.Contains(string(rep), "gate-done") {
			t.Fatalf("report lacks the gate's own output: %q", rep)
		}
		if rec, _ := readGateRecord(s.runDir()); !rec.Delivered {
			t.Fatalf("result must be delivered once: %+v", rec)
		}
	})

	t.Run("finished while the daemon was down", func(t *testing.T) {
		s, m1 := newGateScenario(t, 0)
		g := s.startGate(t, m1)
		recBefore, _ := readGateRecord(s.runDir())
		s.markRunningAndStop(t, m1, g)

		// The gate ends with nobody waiting; only its result files remain.
		s.releaseGate()
		waitUntil(t, "the gate's exit file", func() bool {
			_, err := os.Stat(filepath.Join(s.runDir(), verifyExitFile))
			return err == nil
		})
		waitUntil(t, "the gate process to be gone", func() bool { return !gateRecordAlive(recBefore) })

		m2 := s.manager(t)
		prd2, task2 := s.load(t, m2)
		m2.resetInProgressTasksForResume(prd2)
		if task2.Status != TaskPending || task2.RetryCount != 0 {
			t.Fatalf("a finished-but-undelivered gate must not fail the task: status=%q retries=%d", task2.Status, task2.RetryCount)
		}
		handled, err := m2.runScriptGateTask(context.Background(), prd2, task2)
		if !handled || err != nil {
			t.Fatalf("runScriptGateTask = (%v, %v)", handled, err)
		}
		got, _ := reloadedTask(t, m2, s.prdID, s.taskID)
		if got.Status != TaskCompleted {
			t.Fatalf("status = %q, want completed from the result file", got.Status)
		}
		if n := s.runs(t); n != 1 {
			t.Fatalf("gate ran %d times, want 1 (result read, not rerun)", n)
		}
		if rec, _ := readGateRecord(s.runDir()); !rec.Delivered {
			t.Fatalf("result must be delivered once: %+v", rec)
		}
	})
}

// fakeClockAt is the injectable fake clock the drain and retry tests use.
func fakeClockAt(t time.Time) *restartFakeClock { return &restartFakeClock{now: t} }

func TestGateDrainWaitsAndTimesOut(t *testing.T) {
	base := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

	t.Run("waits while a gate runs and returns when it ends", func(t *testing.T) {
		s, m := newGateScenario(t, 10)
		g := s.startGate(t, m)
		clk := fakeClockAt(base)
		clk.onSleep = func(n int) {
			if n == 2 {
				s.releaseGate()
				waitUntil(t, "the gate to end", func() bool { return len(m.RunningGates()) == 0 })
			}
		}
		res := m.DrainGates(context.Background(), clk, nil)
		if res.Waited != 1 || res.Interrupted != 0 || res.TimedOut {
			t.Fatalf("drain result = %+v, want 1 waited, 0 interrupted, no timeout", res)
		}
		if clk.sleeps != 2 {
			t.Fatalf("drain polled %d times, want 2 (returns as the gate ends)", clk.sleeps)
		}
		if o := g.wait(t); !o.handled || o.err != nil {
			t.Fatalf("gate task: %+v", o)
		}
	})

	t.Run("fake clock passes the limit and the gate is left running", func(t *testing.T) {
		s, m := newGateScenario(t, 2)
		_ = s.startGate(t, m)
		rec, _ := readGateRecord(s.runDir())
		clk := fakeClockAt(base)
		res := m.DrainGates(context.Background(), clk, nil)
		if res.Waited != 1 || res.Interrupted != 1 || !res.TimedOut {
			t.Fatalf("drain result = %+v, want 1 waited, 1 interrupted, timed out", res)
		}
		if res.Elapsed < 2*time.Minute || res.Elapsed > 2*time.Minute+time.Second {
			t.Fatalf("elapsed on the fake clock = %s, want about the 2-minute limit", res.Elapsed)
		}
		if !gateRecordAlive(rec) || len(m.RunningGates()) != 1 {
			t.Fatal("the wait must end cleanly and leave the gate running, never signalled")
		}
	})

	t.Run("setting 0 does not wait", func(t *testing.T) {
		s, m := newGateScenario(t, 0)
		_ = s.startGate(t, m)
		clk := fakeClockAt(base)
		res := m.DrainGates(context.Background(), clk, nil)
		if res.Waited != 0 || res.Interrupted != 1 || res.TimedOut || clk.sleeps != 0 || m.LaunchDraining() {
			t.Fatalf("drain result = %+v sleeps=%d draining=%v, want no wait", res, clk.sleeps, m.LaunchDraining())
		}
	})
}

// gateRetryScenario builds the gate fixture with the daemon-start time and
// the fake clock set so the kill lands sinceStart after the daemon started.
func gateRetryScenario(t *testing.T, sinceStart time.Duration) (*Manager, *PRD, *Task) {
	t.Helper()
	m, prd, task, _ := scriptGateFixture(t, ".datawatch/verify/gate.sh", "exit 0\n", 30)
	base := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	clk := fakeClockAt(base.Add(sinceStart))
	m.startedAt = base
	m.genArt.now = clk.Now
	return m, prd, task
}

func TestGateRestartRetryOnce(t *testing.T) {
	calls := gateRestartFake(t, gateKilled, GateResult{Passed: true, Output: "second run\n"})
	m, prd, task := gateRetryScenario(t, 3*time.Minute)
	ceiling := prd.RetryCeilingTotal
	if err := runScriptGate(t, m, prd, task); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("gate ran %d times, want 2 (killed, then retried once)", *calls)
	}
	got, _ := reloadedTask(t, m, prd.ID, task.ID)
	if got.Status != TaskCompleted || !got.GateRestartRetryUsed {
		t.Fatalf("status=%q retryUsed=%v", got.Status, got.GateRestartRetryUsed)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	if got.RetryCount != 0 || len(p.RetryCountsByCategory) != 0 || p.RetryCeilingTotal != ceiling {
		t.Fatalf("budget changed: RetryCount=%d byCategory=%v ceiling %d->%d",
			got.RetryCount, p.RetryCountsByCategory, ceiling, p.RetryCeilingTotal)
	}
	named := false
	for _, d := range p.Decisions {
		if d.Kind == gateRestartRetryDecisionKind && strings.Contains(d.Note, "daemon restarted") {
			named = true
		}
	}
	if !named {
		t.Fatalf("no decision-log line naming the restart: %+v", p.Decisions)
	}
}

func TestGateKilledLongAfterRestartIsFailure(t *testing.T) {
	calls := gateRestartFake(t, gateKilled, GateResult{Passed: true})
	m, prd, task := gateRetryScenario(t, gateRestartRecentWindow+10*time.Minute)
	if err := runScriptGate(t, m, prd, task); err == nil {
		t.Fatal("want an ordinary gate failure")
	}
	got, _ := reloadedTask(t, m, prd.ID, task.ID)
	if *calls != 1 || got.Status != TaskFailed || got.GateRestartRetryUsed {
		t.Fatalf("calls=%d status=%q retryUsed=%v, want 1 call, failed, no retry", *calls, got.Status, got.GateRestartRetryUsed)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	for _, d := range p.Decisions {
		if d.Kind == gateRestartRetryDecisionKind {
			t.Fatalf("a late kill must not log a restart retry: %+v", d)
		}
	}
}
