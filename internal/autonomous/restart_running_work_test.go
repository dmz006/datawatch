// Combined restart cases for running work: a launch refused while the daemon
// drains, a live session adopted instead of respawned, and two sessions for
// one task. Fakes only; nothing here starts a daemon or touches tmux.
package autonomous

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// (1) Restart during launch: the refusal is a wait, not an attempt.
func TestRestartRunningWork_LaunchRefusedWhileDrainingIsNotAnAttempt(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	before := prd.Story[0].Tasks[0]

	old := capRetryPause
	capRetryPause = 10 * time.Millisecond
	defer func() { capRetryPause = old }()

	var mu sync.Mutex
	calls := 0
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return SpawnResult{}, session.ErrDaemonDraining
		}
		return SpawnResult{SessionID: "s-after-restart"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	tk := got.Story[0].Tasks[0]

	if got.Status != PRDCompleted || calls != 2 {
		t.Fatalf("status=%s calls=%d, want completed after the refused launch and one real launch", got.Status, calls)
	}
	if tk.RetryCount != before.RetryCount || tk.InfraRetryCount != 0 || len(tk.InfraRetries) != 0 ||
		tk.ClaudeRetries != 0 || tk.StartFailures != 0 {
		t.Errorf("a retry counter moved: %+v", tk)
	}
	if len(tk.Attempts) > 1 {
		t.Errorf("the refusal produced an extra attempt record: %+v", tk.Attempts)
	}
	for _, a := range tk.Attempts {
		if a.Outcome != "" && a.Outcome != "completed" && a.Outcome != "passed" {
			t.Errorf("refusal left a failed attempt: %+v", a)
		}
	}
}

// The task returns to waiting while the daemon is going down and the counters
// and Attempts stay exactly as they were.
func TestRestartRunningWork_RefusedLaunchLeavesCountersAndAttemptsUnchanged(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	tk := &prd.Story[0].Tasks[0]
	tk.RetryCount, tk.InfraRetryCount = 2, 1
	tk.Attempts = []TaskAttempt{{Number: 1, Outcome: "failed"}}
	snapRetry, snapInfra, snapAttempts := tk.RetryCount, tk.InfraRetryCount, len(tk.Attempts)

	old := capRetryPause
	capRetryPause = 10 * time.Millisecond
	defer func() { capRetryPause = old }()

	ctx, cancel := context.WithCancelCause(context.Background())
	calls := 0
	spawn := m.capacityRetrySpawn(tk, func(context.Context, SpawnRequest) (SpawnResult, error) {
		calls++
		if calls == 2 {
			cancel(ErrDaemonRestart)
		}
		return SpawnResult{}, session.ErrDaemonDraining
	})
	if _, err := spawn(ctx, SpawnRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context cancellation", err)
	}
	if tk.RetryCount != snapRetry || tk.InfraRetryCount != snapInfra || len(tk.Attempts) != snapAttempts ||
		len(tk.InfraRetries) != 0 || tk.StartFailures != 0 || tk.ClaudeRetries != 0 {
		t.Errorf("counters or Attempts changed: %+v", tk)
	}
}

// (2) Restart with a live session: adopted, no second spawn, a worktree with
// uncommitted changes is never reset, the attempt number is kept.
func TestRestartRunningWork_LiveSessionAdoptedNoSecondSpawn(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk0 := &cur.Story[0].Tasks[0]
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "sess-live", TaskID: tk0.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-live"}))
	var dirtyChecks int32
	m.SetWorktreeDirtyFn(func(string) (bool, error) { atomic.AddInt32(&dirtyChecks, 1); return true, nil })

	var spawns int32
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawns, 1)
		return SpawnResult{}, errors.New("must not spawn beside a live session")
	}
	out := runToTerminal(t, m, api, cur.ID, spawn, verify)
	tk := out.Story[0].Tasks[0]

	if out.Status != PRDCompleted || tk.Status != TaskCompleted {
		t.Fatalf("prd=%s task=%s err=%q", out.Status, tk.Status, tk.Error)
	}
	if n := atomic.LoadInt32(&spawns); n != 0 {
		t.Fatalf("spawned %d worker(s), want 0", n)
	}
	if tk.SessionID != "sess-live" {
		t.Errorf("session=%q, want sess-live", tk.SessionID)
	}
	if fs.killCount() != 0 {
		t.Errorf("a session was ended: %v", fs.killed())
	}
	if tk.RetryCount != 0 || tk.PriorAttemptCount != 0 {
		t.Errorf("attempt number moved: retry=%d prior=%d", tk.RetryCount, tk.PriorAttemptCount)
	}
}

func TestRestartRunningWork_DirtyWorktreeNeverResetOverLiveSession(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	tk.RetryCount = 1
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "sess-live", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-live"}))
	m.SetWorktreeDirtyFn(func(string) (bool, error) { return true, nil })

	err := m.guardLiveWorktree(context.Background(), cur, tk, dir)
	if !errors.Is(err, ErrLiveWorktreeOwned) {
		t.Fatalf("guard err = %v, want ErrLiveWorktreeOwned", err)
	}
	if fs.killCount() != 0 {
		t.Errorf("the live session was ended: %v", fs.killed())
	}
	if tk.RetryCount != 1 {
		t.Errorf("attempt number changed: %d", tk.RetryCount)
	}
	if len(cur.Decisions) == 0 || cur.Decisions[len(cur.Decisions)-1].Kind != "worktree_guard" {
		t.Errorf("refusal not recorded: %v", cur.Decisions)
	}
}

// (3) Two sessions for one task.
func TestRestartRunningWork_TwoSessionsOlderKeptByDefault(t *testing.T) {
	m, prd, tk, f := dupSetup(t, "")
	got, _ := findAdoptableSessionKeep(prd, tk, f.list(), m.duplicateSessionKeep())
	if got == nil || got.ID != "old" {
		t.Fatalf("default adopts %v, want old", got)
	}
	ended := m.endDuplicateSessions(prd, tk, got.ID, "test", "duplicate after restart")
	if len(ended) != 1 || ended[0] != "new" {
		t.Fatalf("ended = %v, want [new]", ended)
	}
	if len(tk.SessionDecisions) != 1 || tk.SessionDecisions[0].Kept != "old" || tk.SessionDecisions[0].Ended[0] != "new" {
		t.Fatalf("decision not recorded: %+v", tk.SessionDecisions)
	}
}

func TestRestartRunningWork_TwoSessionsNewerKeptWhenOverridden(t *testing.T) {
	m, prd, tk, f := dupSetup(t, "newer")
	got, _ := findAdoptableSessionKeep(prd, tk, f.list(), m.duplicateSessionKeep())
	if got == nil || got.ID != "new" {
		t.Fatalf("override adopts %v, want new", got)
	}
	ended := m.endDuplicateSessions(prd, tk, got.ID, "test", "duplicate after restart")
	if len(ended) != 1 || ended[0] != "old" {
		t.Fatalf("ended = %v, want [old]", ended)
	}
	if len(tk.SessionDecisions) != 1 || tk.SessionDecisions[0].Kept != "new" || tk.SessionDecisions[0].Ended[0] != "old" {
		t.Fatalf("decision not recorded: %+v", tk.SessionDecisions)
	}
	if len(f.audits) != 1 || f.audits[0] != auditActionDuplicateSession {
		t.Errorf("audits = %v", f.audits)
	}
}
