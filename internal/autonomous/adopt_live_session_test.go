// Boot-resume session adoption, end to end within the autonomous package:
//
//   - ALIVE session on an in_progress task  → adopted: kept in_progress,
//     session ID preserved, capacity lease re-taken, never killed, and the
//     second boot pass does not double-adopt (no duplicate lease).
//   - DEAD/EMPTY session on an in_progress task (no completion marker) →
//     reset to pending, session cleared, leftover session killed.
//   - DEAD session whose recorded output carries the completion marker →
//     finished while the daemon was down: moved to verifying, verified
//     exactly once, and the executor NEVER spawns a second worker.
//   - ADOPTED in_progress task running through the executor → it waits on
//     the fake session completion instead of spawning; concurrent Run calls
//     for the same PRD start exactly ONE executor (claimRun guard).
//
// All fakes are table/map backed; waits are bounded polls, no fixed sleeps.
package autonomous

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSessions implements the Manager's session hooks (alive / output /
// kill / wait) against simple maps and records the kill and wait calls so
// tests can assert exactly which sessions were touched.
type fakeSessions struct {
	mu      sync.Mutex
	alive   map[string]bool
	output  map[string]string // recorded output text, present = recorded
	kills   []string
	waits   int
	waitErr error
}

func newFakeSessions(live ...string) *fakeSessions {
	f := &fakeSessions{alive: map[string]bool{}, output: map[string]string{}}
	for _, id := range live {
		f.alive[id] = true
	}
	return f
}

func (f *fakeSessions) recordOutput(sessionID, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.output[sessionID] = text
}

func (f *fakeSessions) wire(m *Manager) {
	m.SetSessionAliveFn(f.aliveFn)
	m.SetSessionKillerFn(f.killFn)
	m.SetSessionOutputFn(f.outputFn)
	m.SetSessionWaitFn(f.waitFn)
}

func (f *fakeSessions) aliveFn(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[sessionID]
}

func (f *fakeSessions) killFn(sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kills = append(f.kills, sessionID)
	return nil
}

func (f *fakeSessions) outputFn(sessionID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out, ok := f.output[sessionID]
	return out, ok
}

func (f *fakeSessions) waitFn(ctx context.Context, sessionID string) error {
	f.mu.Lock()
	f.waits++
	err := f.waitErr
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return err
}

func (f *fakeSessions) killed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.kills...)
}

func (f *fakeSessions) killCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.kills)
}

func (f *fakeSessions) waitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waits
}

// setupBootedAdoptState creates a PRD with one in_progress task carrying the
// given session ID, leaves the PRD in PRDRunning (the state the daemon's
// store is in when it goes down mid-run), and returns a fresh PRD snapshot.
// The PRD is pointed at a real project directory so the boot-resume gate
// (which parks PRDs whose work directory is gone) can never interfere with
// the adoption decision under test.
func setupBootedAdoptState(t *testing.T, m *Manager, sessionID string) *PRD {
	t.Helper()
	projDir := t.TempDir() // boot-resume gate requires the work dir to exist
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskInProgress
	tk.SessionID = sessionID
	if err := m.Store().SaveTask(tk); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning // daemon died mid-run: executor goroutines lost
	cur.ProjectDir = projDir
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	return cur
}

// TestAdoptLive_AliveAdopted: an in_progress task whose worker session
// survived the daemon restart is adopted at boot-resume: it stays in_progress
// with the same session ID, its capacity lease is re-taken (so a sibling
// cannot claim the slot), it is never killed, and a second boot pass is
// idempotent (still exactly one lease).
func TestAdoptLive_AliveAdopted(t *testing.T) {
	m, _, led := capFixture(t, 1)
	fs := newFakeSessions("sess-live")
	fs.wire(m)

	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	live := &cur.Story[0].Tasks[0]
	live.Status = TaskInProgress
	live.SessionID = "sess-live"
	if err := m.Store().SaveTask(live); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)

	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskInProgress {
		t.Fatalf("adopted task must stay in_progress, got %s", tk.Status)
	}
	if tk.SessionID != "sess-live" {
		t.Fatalf("adopted task must keep its session, got %q", tk.SessionID)
	}
	if !tk.AdoptedSession {
		t.Fatal("adopted task must carry AdoptedSession so executeOne waits instead of spawning")
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("the live worker session must not be killed on adopt, killed %v", fs.killed())
	}
	if !led.Held(tk.ID) {
		t.Fatal("adopted task holds no capacity lease after boot-resume")
	}
	if n := len(led.Snapshot().Leases); n != 1 {
		t.Fatalf("want exactly one lease after boot, got %d", n)
	}

	// The slot is taken: the sibling task must not be admitted over it.
	if led.Held(got.Story[0].Tasks[1].ID) {
		t.Fatal("sibling task holds a lease it never acquired")
	}

	// Idempotency: a second boot pass must not double-adopt or reset a
	// live, already-adopted task.
	m.resetInProgressTasksForResume(got)
	got, _ = m.Store().GetPRD(prd.ID)
	tk = got.Story[0].Tasks[0]
	if !led.Held(tk.ID) {
		t.Fatal("second boot pass dropped the adopted lease")
	}
	if n := len(led.Snapshot().Leases); n != 1 {
		t.Fatalf("second boot pass must not add a lease (got %d total)", n)
	}
	if tk.Status != TaskInProgress || tk.SessionID != "sess-live" || !tk.AdoptedSession {
		t.Fatalf("second boot pass disturbed the adopted task: status=%s session=%q adopted=%v", tk.Status, tk.SessionID, tk.AdoptedSession)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("second boot pass killed the live session: %v", fs.killed())
	}
}

// TestAdoptLive_DeadReset: an in_progress task whose session is dead (or was
// never recorded) and has no completion marker is reset to pending, its
// session reference is cleared, and a recorded dead session is killed.
func TestAdoptLive_DeadReset(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sessionID  string
		recorded   bool // session output was recorded (progress log, no marker)
		wantKilled int
	}{
		{name: "dead_session", sessionID: "sess-dead", recorded: true, wantKilled: 1},
		{name: "empty_session", sessionID: "", recorded: false, wantKilled: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _ := apiFixture(t)
			fs := newFakeSessions() // nothing is alive
			if tc.recorded {
				fs.recordOutput("sess-dead", "worker progress log so far (no completion marker)")
			}
			fs.wire(m)

			cur := setupBootedAdoptState(t, m, tc.sessionID)
			m.resetInProgressTasksForResume(cur)

			got, _ := m.Store().GetPRD(cur.ID)
			tk := got.Story[0].Tasks[0]
			if tk.Status != TaskPending {
				t.Fatalf("dead/empty session task must reset to pending, got %s", tk.Status)
			}
			if tk.SessionID != "" {
				t.Fatalf("reset task must clear its session reference, got %q", tk.SessionID)
			}
			if tk.AdoptedSession {
				t.Fatal("reset task must not carry AdoptedSession")
			}
			if n := fs.killCount(); n != tc.wantKilled {
				t.Fatalf("kill calls = %v, want %d for case %s", fs.killed(), tc.wantKilled, tc.name)
			}
			if tc.name == "dead_session" {
				kills := fs.killed()
				if kills[0] != "sess-dead" {
					t.Fatalf("wrong session killed: %v", kills)
				}
			}
		})
	}
}

// TestAdoptLive_FinishedWhileDown: an in_progress task whose session ended
// while the daemon was down, with the completion marker in its recorded
// output, moves straight to verifying at boot (session kept, never killed).
// The executor then verifies the recorded work exactly once and NEVER spawns
// a second worker alongside the finished one.
func TestAdoptLive_FinishedWhileDown(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	fs := newFakeSessions() // session is dead
	fs.recordOutput("sess-done", "built and tested\ndata done\nDATAWATCH_COMPLETE: finished the work\n")
	fs.wire(m)

	cur := setupBootedAdoptState(t, m, "sess-done")
	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskVerifying {
		t.Fatalf("finished-while-down task must move to verifying, got %s", tk.Status)
	}
	if tk.SessionID != "sess-done" {
		t.Fatalf("finished-while-down task must keep its session for verification, got %q", tk.SessionID)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("finished session must not be killed (its output must be verified), killed %v", fs.killed())
	}

	var spawnN, verifyN int32
	countingSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawnN, 1)
		return SpawnResult{}, fmt.Errorf("must not spawn: task finished while the daemon was down")
	}
	countingVerify := func(ctx context.Context, p *PRD, tk *Task) (VerificationResult, error) {
		atomic.AddInt32(&verifyN, 1)
		return verify(ctx, p, tk)
	}

	out := runToTerminal(t, m, api, cur.ID, countingSpawn, countingVerify)
	if out.Status != PRDCompleted {
		t.Fatalf("want PRDCompleted, got %s (task error: %q)", out.Status, out.Story[0].Tasks[0].Error)
	}
	if st := out.Story[0].Tasks[0].Status; st != TaskCompleted {
		t.Fatalf("task should be completed, got %s", st)
	}
	if n := atomic.LoadInt32(&spawnN); n != 0 {
		t.Fatalf("executor spawned %d worker(s) for finished work; want 0 (no duplicate spawn)", n)
	}
	if n := atomic.LoadInt32(&verifyN); n != 1 {
		t.Fatalf("verify called %d times; want exactly 1 (the recorded output is verified once)", n)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("finished session killed %d time(s); want 0", n)
	}
}

// TestAdoptLive_NoDuplicateSpawn: an in_progress task adopted at boot must
// run through the executor without a new spawn (it waits on the adopted
// session), and concurrent Run calls for the same PRD must start exactly one
// executor — the claimRun guard keeps the second call a no-op.
func TestAdoptLive_NoDuplicateSpawn(t *testing.T) {
	t.Run("adopted_task_waits_instead_of_spawning", func(t *testing.T) {
		m, api, _, verify := apiFixture(t)
		fs := newFakeSessions("sess-live") // adopted session is alive; wait hook completes it
		fs.wire(m)

		cur := setupBootedAdoptState(t, m, "sess-live")
		m.resetInProgressTasksForResume(cur)

		got, _ := m.Store().GetPRD(cur.ID)
		tk := got.Story[0].Tasks[0]
		if tk.Status != TaskInProgress || !tk.AdoptedSession {
			t.Fatalf("precondition: task must be adopted (status=%s adopted=%v)", tk.Status, tk.AdoptedSession)
		}

		var spawnN, verifyN int32
		countingSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
			atomic.AddInt32(&spawnN, 1)
			return SpawnResult{}, fmt.Errorf("must not spawn a second worker for an adopted live session")
		}
		countingVerify := func(ctx context.Context, p *PRD, k *Task) (VerificationResult, error) {
			atomic.AddInt32(&verifyN, 1)
			return verify(ctx, p, k)
		}

		out := runToTerminal(t, m, api, cur.ID, countingSpawn, countingVerify)
		if out.Status != PRDCompleted {
			t.Fatalf("want PRDCompleted, got %s (task error: %q)", out.Status, out.Story[0].Tasks[0].Error)
		}
		if st := out.Story[0].Tasks[0].Status; st != TaskCompleted {
			t.Fatalf("task should be completed, got %s", st)
		}
		if n := fs.waitCount(); n != 1 {
			t.Fatalf("executor must wait once for the adopted session's completion, waited %d", n)
		}
		if n := atomic.LoadInt32(&verifyN); n != 1 {
			t.Fatalf("verify called %d times; want exactly 1 (after the fake completion)", n)
		}
		if n := atomic.LoadInt32(&spawnN); n != 0 {
			t.Fatalf("executor spawned %d worker(s) for an adopted live session; want 0", n)
		}
		if n := fs.killCount(); n != 0 {
			t.Fatalf("adopted live session killed %d time(s); want 0 (killed %v)", n, fs.killed())
		}
	})

	t.Run("concurrent_run_starts_one_executor", func(t *testing.T) {
		m, api, _, verify := apiFixture(t)
		fs := newFakeSessions("sess-live")
		fs.wire(m)

		cur := setupBootedAdoptState(t, m, "sess-live")
		m.resetInProgressTasksForResume(cur)

		var spawnN int32
		countingSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
			atomic.AddInt32(&spawnN, 1)
			return SpawnResult{}, fmt.Errorf("must not spawn a second worker for an adopted live session")
		}
		ready := make(chan struct{})
		api.SetServerReadyCh(ready)
		api.SetExecutors(countingSpawn, verify) // boot-resume scan is parked on ready

		// Two concurrent Run calls for the same PRD: the claimRun guard must
		// let exactly one executor start; the other returns a safe no-op. The
		// boot-resume scan stays parked (as TestRunDedupe does) because a truly
		// simultaneous scan and Run is a documented data race in the scan
		// itself, not in the dedupe check this test exercises.
		errs := make([]error, 2)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = api.Run(cur.ID)
			}(i)
		}
		close(start)
		wg.Wait()
		for i, e := range errs {
			if e != nil {
				t.Fatalf("concurrent api.Run %d: %v", i, e)
			}
		}

		pollPRDTerminal(t, m, cur.ID, 15*time.Second)
		if n := fs.waitCount(); n != 1 {
			t.Fatalf("concurrent runLocked calls must start exactly one executor (wait hook called %d times, want 1)", n)
		}
		if n := atomic.LoadInt32(&spawnN); n != 0 {
			t.Fatalf("duplicate worker: %d spawn(s) for one adopted session, want 0", n)
		}
		if n := fs.killCount(); n != 0 {
			t.Fatalf("adopted live session killed %d time(s); want 0 (killed %v)", n, fs.killed())
		}

		// Release the parked boot-resume scan: with the executor slot claimed
		// (or already finished and completed) it must not start a second
		// executor or disturb the outcome.
		close(ready)
		if err := api.WaitIdle(10 * time.Second); err != nil {
			t.Fatal(err)
		}
		got, _ := m.Store().SnapshotPRD(cur.ID)
		if got.Status != PRDCompleted {
			t.Fatalf("want PRDCompleted, got %s (task error: %q)", got.Status, got.Story[0].Tasks[0].Error)
		}
		if n := fs.waitCount(); n != 1 {
			t.Fatalf("boot-resume scan must not start a second executor (wait hook called %d times, want 1)", n)
		}
		if n := atomic.LoadInt32(&spawnN); n != 0 {
			t.Fatalf("duplicate worker: %d spawn(s) for one adopted session, want 0", n)
		}
		if n := fs.killCount(); n != 0 {
			t.Fatalf("adopted live session killed %d time(s); want 0 (killed %v)", n, fs.killed())
		}
	})
}
