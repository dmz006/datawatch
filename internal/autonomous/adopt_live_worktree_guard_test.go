package autonomous

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// listerFor returns a session lister backed by fs: a session is Alive while fs
// says so, so a kill through fs.killFn shows up in the next listing.
func listerFor(fs *fakeSessions, infos ...SessionInfo) func() []SessionInfo {
	return func() []SessionInfo {
		out := make([]SessionInfo, 0, len(infos))
		for _, s := range infos {
			s.Alive = fs.aliveFn(s.ID)
			out = append(out, s)
		}
		return out
	}
}

// pendingTaskWithDir makes the single task pending and returns the PRD plus
// the work dir the executor resolves for it.
func pendingTaskWithDir(t *testing.T, m *Manager) (*PRD, string) {
	t.Helper()
	cur := setupBootedAdoptState(t, m, "")
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskPending
	tk.SessionID = ""
	if err := m.Store().SaveTask(tk); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	cur, _ = m.Store().GetPRD(cur.ID)
	return cur, m.resolveTaskProjectDir(cur, &cur.Story[0].Tasks[0])
}

func TestAdoptLiveWorktree_AdoptsInsteadOfSpawning(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	taskID := cur.Story[0].Tasks[0].ID
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "sess-live", TaskID: taskID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-live"}))

	var spawnN int32
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawnN, 1)
		return SpawnResult{}, errors.New("must not spawn beside a live worker")
	}
	out := runToTerminal(t, m, api, cur.ID, spawn, verify)
	tk := out.Story[0].Tasks[0]
	if out.Status != PRDCompleted || tk.Status != TaskCompleted {
		t.Fatalf("want completed, got prd=%s task=%s err=%q", out.Status, tk.Status, tk.Error)
	}
	if atomic.LoadInt32(&spawnN) != 0 {
		t.Fatalf("spawned %d worker(s); want 0", spawnN)
	}
	if fs.waitCount() != 1 || fs.killCount() != 0 {
		t.Fatalf("waits=%d kills=%v; want one wait and no kill", fs.waitCount(), fs.killed())
	}
	if tk.SessionID != "sess-live" {
		t.Fatalf("task session = %q, want sess-live", tk.SessionID)
	}
	if tk.RetryCount != 0 || tk.PriorAttemptCount != 0 {
		t.Fatalf("adoption changed counters: retry=%d prior=%d", tk.RetryCount, tk.PriorAttemptCount)
	}
}

func TestAdoptLiveWorktree_NoAdoptWithoutTmuxOrWrongDir(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("a", "b")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	m.SetSessionListerFn(listerFor(fs,
		SessionInfo{ID: "a", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir},                                   // no tmux session
		SessionInfo{ID: "b", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir + "-other", TmuxSession: "tmux-b"}, // other worktree
	))
	if id := m.adoptLiveWorkerBeforeSpawn(cur, tk, 0, dir); id != "" {
		t.Fatalf("adopted %q; want none", id)
	}
}

func TestAdoptLiveWorktree_SameSessionAfterFailedAttemptNotAdopted(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("old")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	tk.SessionID = "old"
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "old", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-old"}))
	if id := m.adoptLiveWorkerBeforeSpawn(cur, tk, 1, dir); id != "" {
		t.Fatalf("adopted the ended attempt's own session %q", id)
	}
	if id := m.adoptLiveWorkerBeforeSpawn(cur, tk, 0, dir); id != "old" {
		t.Fatalf("attempt 0 adoption = %q, want old", id)
	}
	if !tk.AdoptedSession || tk.RetryCount != 0 {
		t.Fatalf("adopted=%v retry=%d", tk.AdoptedSession, tk.RetryCount)
	}
}

func TestAdoptLiveWorktree_GuardRefusesDirtyWorktree(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "sess-live", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-live"}))
	m.SetWorktreeDirtyFn(func(string) (bool, error) { return true, nil })

	err := m.guardLiveWorktree(context.Background(), cur, tk, dir)
	if !errors.Is(err, ErrLiveWorktreeOwned) {
		t.Fatalf("guard err = %v; want ErrLiveWorktreeOwned", err)
	}
	if fs.killCount() != 0 {
		t.Fatalf("guard killed %v; want no kill", fs.killed())
	}
	if len(cur.Decisions) == 0 || cur.Decisions[len(cur.Decisions)-1].Kind != "worktree_guard" || tk.Error == "" {
		t.Fatalf("refusal not recorded: decisions=%v err=%q", cur.Decisions, tk.Error)
	}
}

func TestAdoptLiveWorktree_GuardAllowsCleanWorktreeAndNoOwner(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "sess-live", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-live"}))
	m.SetWorktreeDirtyFn(func(string) (bool, error) { return false, nil })
	if err := m.guardLiveWorktree(context.Background(), cur, tk, dir); err != nil {
		t.Fatalf("clean worktree refused: %v", err)
	}
	// A dirty tree with no live owner is not guarded.
	m.SetWorktreeDirtyFn(func(string) (bool, error) { return true, nil })
	fs.mu.Lock()
	fs.alive["sess-live"] = false
	fs.mu.Unlock()
	if err := m.guardLiveWorktree(context.Background(), cur, tk, dir); err != nil {
		t.Fatalf("no live owner but guard refused: %v", err)
	}
}

func TestAdoptLiveWorktree_DirtyCheckErrorCountsAsDirty(t *testing.T) {
	m := capFixtureManager(t)
	m.SetWorktreeDirtyFn(func(string) (bool, error) { return false, errors.New("git failed") })
	if !m.worktreeHasUncommitted(context.Background(), t.TempDir()) {
		t.Fatal("a failed dirty check must count as dirty")
	}
}

func TestAdoptLiveWorktree_CleanRestartEndsSessionThroughKiller(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	m.SetSessionKillerFn(func(id, caller, _ string) error {
		fs.mu.Lock()
		fs.alive[id] = false
		fs.kills = append(fs.kills, id)
		fs.mu.Unlock()
		if caller != "autonomous.executor.clean-restart" {
			t.Errorf("killer caller = %q", caller)
		}
		return nil
	})
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	tk.CleanRestart = true
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "sess-live", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-live"}))
	m.SetWorktreeDirtyFn(func(string) (bool, error) { return true, nil })

	// A clean restart is never adopted over...
	if id := m.adoptLiveWorkerBeforeSpawn(cur, tk, 0, dir); id != "" {
		t.Fatalf("adopted %q despite CleanRestart", id)
	}
	// ...and the guard ends the live session first, even on a dirty tree.
	if err := m.guardLiveWorktree(context.Background(), cur, tk, dir); err != nil {
		t.Fatalf("guard on clean restart: %v", err)
	}
	if got := fs.killed(); len(got) != 1 || got[0] != "sess-live" {
		t.Fatalf("kills = %v; want [sess-live]", got)
	}
	if tk.SessionEndReason == "" {
		t.Fatal("end of the live session was not recorded")
	}
}

func capFixtureManager(t *testing.T) *Manager {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	return m
}
