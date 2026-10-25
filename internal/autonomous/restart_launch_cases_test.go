// Restart during a session launch, on fakes only.
//
//   - bare shell at boot (agent never started) → ended through the killer and
//     the task reset as a launch failure; the retry count is left unchanged.
//   - agent running with no first turn at boot → adopted, then failed fast by
//     the start-confirm bound measured from adoption.
//   - healthy session at boot → adopted, kept, never killed.
//   - launch still in flight at shutdown → either it finishes inside the grace
//     and the next boot adopts it, or it is rolled back (session ended, task
//     link cleared) and the next boot does not adopt the dead session.
//
// The shutdown grace and the in-flight registry live in internal/session
// (launch_registry.go, covered by TestLaunchRegistry*). This package has no
// drain hook, so the grace outcome is modelled as the agent state the next boot
// sees, and the rollback as a call to the session killer hook.
//
// Fakes and helpers come from the earlier boot-resume tests: fakeSessions and
// setupBootedAdoptState (adopt_live_session_test.go), capFixture
// (capacity_test.go), and the start-confirm fakes adoptStepClock,
// blockUntilCancelled and the adoptedTestPane* panes (adopted_start_confirm_test.go).
package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRestartLaunchCases(t *testing.T) {
	t.Run("bare shell left by a boot-time launch failure is ended and reset as a launch failure, retry count unchanged", func(t *testing.T) {
		m, _, led := capFixture(t, 1)
		fs := newFakeSessions("sess-shell") // the pane is up, but only a bare shell runs in it
		fs.wire(m)
		var gotCaller, gotReason string
		m.SetSessionKillerFn(func(id, caller, reason string) error {
			gotCaller, gotReason = caller, reason
			return fs.killFn(id, caller, reason)
		})
		m.SetSessionAgentStartedFn(func(id string) bool { return id != "sess-shell" })

		cur := setupBootedAdoptState(t, m, "sess-shell")
		tk := &cur.Story[0].Tasks[0]
		tk.RetryCount = 2
		tk.StartFailures = 1
		if err := m.Store().SaveTask(tk); err != nil {
			t.Fatalf("SaveTask: %v", err)
		}
		cur, _ = m.Store().GetPRD(cur.ID)

		m.resetInProgressTasksForResume(cur)

		got, _ := m.Store().GetPRD(cur.ID)
		tk2 := got.Story[0].Tasks[0]
		if tk2.Status != TaskPending {
			t.Fatalf("bare-shell task must reset to pending, got %s", tk2.Status)
		}
		if tk2.Error != SessionStartFailureReason {
			t.Fatalf("bare-shell task Error = %q, want %q", tk2.Error, SessionStartFailureReason)
		}
		if tk2.SessionID != "" || tk2.AdoptedSession {
			t.Fatalf("reset task must hold no session: session=%q adopted=%v", tk2.SessionID, tk2.AdoptedSession)
		}
		if killed := fs.killed(); len(killed) != 1 || killed[0] != "sess-shell" {
			t.Fatalf("kill calls = %v, want exactly [sess-shell]", killed)
		}
		if gotCaller != "autonomous.boot-resume.start-failed" {
			t.Fatalf("kill caller = %q, want autonomous.boot-resume.start-failed", gotCaller)
		}
		if !strings.Contains(gotReason, "boot-time launch failure") {
			t.Fatalf("kill reason must name the boot-time launch failure, got %q", gotReason)
		}
		if tk2.RetryCount != 2 || tk2.StartFailures != 1 {
			t.Fatalf("retry counters moved on a launch failure: RetryCount=%d StartFailures=%d, want 2 and 1", tk2.RetryCount, tk2.StartFailures)
		}
		if led.Held(tk2.ID) {
			t.Fatal("bare-shell task must not hold a capacity lease after reset")
		}
	})

	t.Run("session with no first turn is adopted at boot, then failed fast by the start-confirm bound measured from adoption", func(t *testing.T) {
		m, _, led := capFixture(t, 1)
		fs := newFakeSessions("sess-warm")
		fs.wire(m)
		var gotCaller string
		m.SetSessionKillerFn(func(id, caller, reason string) error {
			gotCaller = caller
			return fs.killFn(id, caller, reason)
		})
		// The agent process runs in the pane, so the boot gate adopts the session.
		m.SetSessionAgentStartedFn(func(id string) bool { return id == "sess-warm" })
		// The start-confirm watch reads a parked pane with no assistant turn.
		m.SetSessionPaneFn(func(string) (string, error) { return adoptedTestPaneSilent, nil })
		m.SetSessionStateFn(func(string) string { return sessionStateWaitingInput })
		m.SetSessionStartLimit(adoptedTestLimit)

		cur := setupBootedAdoptState(t, m, "sess-warm")
		m.resetInProgressTasksForResume(cur)

		got, _ := m.Store().GetPRD(cur.ID)
		tk := got.Story[0].Tasks[0]
		if tk.Status != TaskInProgress || tk.SessionID != "sess-warm" || !tk.AdoptedSession {
			t.Fatalf("boot must adopt a session whose agent is running: status=%s session=%q adopted=%v", tk.Status, tk.SessionID, tk.AdoptedSession)
		}
		if tk.StartedAt != nil {
			t.Fatalf("task carries a launch time %v; the bound must measure from adoption", tk.StartedAt)
		}
		if !led.Held(tk.ID) {
			t.Fatal("adopted task holds no capacity lease")
		}
		if n := fs.killCount(); n != 0 {
			t.Fatalf("session killed at boot before the bound could judge it: %v", fs.killed())
		}

		// The watch the executor builds for the adopted task, on a fake clock:
		// each probe moves the clock 10s, so reads[0] is adoption.
		clock := &adoptStepClock{at: time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), step: adoptedTestStep}
		sc := m.adoptedStartConfirmFor(&tk)
		if sc == nil {
			t.Fatal("no start-confirm watch built for the adopted session")
		}
		sc.now = clock.now
		sc.interval = time.Millisecond
		err := waitForAdoptedSession(context.Background(), blockUntilCancelled, fs.aliveFn, AdoptedWait{SessionID: "sess-warm"}, sc)
		if !errors.Is(err, ErrSessionNeverStarted) || !errors.Is(err, ErrWorkerStalled) {
			t.Fatalf("want ErrSessionNeverStarted wrapped with ErrWorkerStalled, got %v", err)
		}
		if killed := fs.killed(); len(killed) != 1 || killed[0] != "sess-warm" {
			t.Fatalf("want the never-started session ended exactly once, got %v", killed)
		}
		if gotCaller != adoptedStartCaller {
			t.Fatalf("kill caller = %q, want %q", gotCaller, adoptedStartCaller)
		}
		last := clock.reads[len(clock.reads)-1].Sub(clock.reads[0])
		prev := clock.reads[len(clock.reads)-2].Sub(clock.reads[0])
		if last != 70*time.Second || prev != 60*time.Second {
			t.Fatalf("bound fired at %s from adoption (previous probe %s); want 70s, with 60s not yet past the %s limit", last, prev, adoptedTestLimit)
		}
		if len(sc.failureTail) == 0 {
			t.Fatal("failure tail not recorded for the task")
		}
	})

	t.Run("healthy session at boot is kept, adopted, and never killed", func(t *testing.T) {
		m, _, led := capFixture(t, 1)
		fs := newFakeSessions("sess-ok")
		fs.wire(m)
		m.SetSessionAgentStartedFn(func(id string) bool { return id == "sess-ok" })

		cur := setupBootedAdoptState(t, m, "sess-ok")
		m.resetInProgressTasksForResume(cur)

		got, _ := m.Store().GetPRD(cur.ID)
		tk := got.Story[0].Tasks[0]
		if tk.Status != TaskInProgress || tk.SessionID != "sess-ok" || !tk.AdoptedSession {
			t.Fatalf("healthy session must be adopted in place: status=%s session=%q adopted=%v", tk.Status, tk.SessionID, tk.AdoptedSession)
		}
		if tk.Error != "" {
			t.Fatalf("healthy adopted task must not carry an Error, got %q", tk.Error)
		}
		if n := fs.killCount(); n != 0 {
			t.Fatalf("healthy session must not be killed on adopt, killed %v", fs.killed())
		}
		if !led.Held(tk.ID) {
			t.Fatal("healthy adopted task holds no capacity lease after boot-resume")
		}
	})

	t.Run("launch still running at shutdown finishes inside the grace, and the next boot adopts it", func(t *testing.T) {
		m, _, led := capFixture(t, 1)
		fs := newFakeSessions("sess-launch")
		fs.wire(m)
		// In flight at shutdown: the session is up but its agent has not started.
		agentStarted := false
		m.SetSessionAgentStartedFn(func(id string) bool { return agentStarted && id == "sess-launch" })

		cur := setupBootedAdoptState(t, m, "sess-launch")

		// The agent comes up inside the grace, so the drain has nothing to roll back.
		agentStarted = true

		m.resetInProgressTasksForResume(cur)

		got, _ := m.Store().GetPRD(cur.ID)
		tk := got.Story[0].Tasks[0]
		if tk.Status != TaskInProgress || tk.SessionID != "sess-launch" || !tk.AdoptedSession {
			t.Fatalf("launch that finished inside the grace must be adopted: status=%s session=%q adopted=%v", tk.Status, tk.SessionID, tk.AdoptedSession)
		}
		if n := fs.killCount(); n != 0 {
			t.Fatalf("launch that finished inside the grace was killed: %v", fs.killed())
		}
		if !led.Held(tk.ID) {
			t.Fatal("adopted task holds no capacity lease")
		}
	})

	t.Run("launch still running at the grace deadline is rolled back, and the next boot does not adopt the dead session", func(t *testing.T) {
		m, _, led := capFixture(t, 1)
		fs := newFakeSessions("sess-launch")
		fs.wire(m)
		var rollbackCaller string
		// The killed pane is gone: the session reads as dead from here on.
		m.SetSessionKillerFn(func(id, caller, reason string) error {
			rollbackCaller = caller
			if err := fs.killFn(id, caller, reason); err != nil {
				return err
			}
			fs.mu.Lock()
			fs.alive[id] = false
			fs.mu.Unlock()
			return nil
		})
		m.SetSessionAgentStartedFn(func(string) bool { return false }) // the agent never came up

		cur := setupBootedAdoptState(t, m, "sess-launch")

		// Grace deadline passes with the launch still running: end the session
		// through the killer, then clear the task's link to it.
		m.mu.Lock()
		kill := m.sessionKillerFn
		m.mu.Unlock()
		if err := kill("sess-launch", "session.launch.rollback", "daemon drain: launch still running at the grace deadline"); err != nil {
			t.Fatalf("rollback kill: %v", err)
		}
		live, _ := m.Store().GetPRD(cur.ID)
		lt := &live.Story[0].Tasks[0]
		lt.SessionID = ""
		if err := m.Store().SaveTask(lt); err != nil {
			t.Fatalf("SaveTask: %v", err)
		}
		cur, _ = m.Store().GetPRD(cur.ID)

		m.resetInProgressTasksForResume(cur)

		got, _ := m.Store().GetPRD(cur.ID)
		tk := got.Story[0].Tasks[0]
		if tk.AdoptedSession || tk.SessionID != "" {
			t.Fatalf("dead-on-arrival session was adopted: session=%q adopted=%v", tk.SessionID, tk.AdoptedSession)
		}
		if tk.Status != TaskPending {
			t.Fatalf("rolled-back task must be requeued at boot, got %s", tk.Status)
		}
		if led.Held(tk.ID) {
			t.Fatal("rolled-back task holds a capacity lease after boot")
		}
		if killed := fs.killed(); len(killed) != 1 || killed[0] != "sess-launch" {
			t.Fatalf("kill calls = %v, want exactly the rollback kill of sess-launch", killed)
		}
		if rollbackCaller != "session.launch.rollback" {
			t.Fatalf("rollback kill caller = %q", rollbackCaller)
		}
	})
}
