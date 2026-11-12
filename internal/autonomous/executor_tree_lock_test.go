// BL428 §3a — executor-level tests for the per-canonical-tree write lock
// wired into executeOne's admission path (capacity → dirty-tree gate →
// tree lock → spawn), the refuse_dirty_tree_start gate, and release on
// every exit path.
package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// fastTreeLockPolling sets the lock wait poll interval low for tests and
// restores it after the test ends.
func fastTreeLockPolling(t *testing.T) {
	t.Helper()
	old := capRetryPause
	capRetryPause = 10 * time.Millisecond
	t.Cleanup(func() { capRetryPause = old })
}

func newTreeLockExecutorManager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	dir := t.TempDir()
	cfg.Enabled = true
	mgr, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

// makeTreePRD creates one approved PRD with one task in projectDir and
// returns the PRD plus its task ID.
func makeTreePRD(t *testing.T, mgr *Manager, projectDir string) (*PRD, string) {
	t.Helper()
	st := mgr.Store()
	prd, err := st.CreatePRD("tree lock test", projectDir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "do it"}},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD")
	}
	prd.Status = PRDApproved
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskID := prd.Story[0].Tasks[0].ID
	if taskID == "" {
		t.Fatal("task ID not assigned")
	}
	return prd, taskID
}

func lockPathFor(t *testing.T, prd *PRD) string {
	t.Helper()
	key, root, err := ResolveTreeKey(prd, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	return treeLockPath(key, root)
}

func okSpawn(_ context.Context, req SpawnRequest) (SpawnResult, error) {
	return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
}

// gitRepoWithDWSeed is newTestRepo plus the scratch patterns datawatch
// seeds into .gitignore once per PRD at run start (executor.go's
// EnsureIgnoredPatterns) pre-committed — mirroring an established
// project where that one-time seam already fired, so a fresh run does
// not mutate the tree and the dirty-detection under test sees exactly
// the dirt the test intentionally leaves.
func gitRepoWithDWSeed(t *testing.T) string {
	t.Helper()
	repo := newTestRepo(t)
	src := ".datawatch/\n.decompose-output-*.json\nCHECKPOINT.md\n"
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(src), 0o644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}
	gitRun(t, repo, "add", ".gitignore")
	gitRun(t, repo, "commit", "-q", "-m", "seed patterns")
	return repo
}

func tlVerify(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
	return VerificationResult{OK: true, Summary: "ok"}, nil
}

// TestExecutorTreeLockSerializesSameTree verifies that two concurrent
// tasks on the same canonical tree spawn strictly one after the other:
// while the first task still runs, the second task is admitted into
// executeOne (capacity is separate) but waits on the tree lock, and its
// spawn does not begin until the first task's deferred release fires.
func TestExecutorTreeLockSerializesSameTree(t *testing.T) {
	// Note: two concurrent tasks on one store also trip a
	// pre-existing manager-internal PRD persistence race (present on
	// the clean tree, e.g. TestExecutor_ContinueOnStoryFailure_PRDOverride
	// under -race). That race is out of BL428 scope; this test asserts
	// the lock ordering itself and is green without -race.
	fastTreeLockPolling(t)
	projDir := t.TempDir()
	mgr := newTreeLockExecutorManager(t, Config{})
	st := mgr.Store()

	prd, err := st.CreatePRD("two tasks", projDir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{
			{Title: "T1", Spec: "one"},
			{Title: "T2", Spec: "two"},
		},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD")
	}
	prd.Status = PRDApproved
	prd.MaxConcurrentTasks = 2
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	type spawnEv struct {
		id     string
		start  time.Time
		end    time.Time
		spawnN int
	}
	var mu sync.Mutex
	var events []spawnEv
	releaseFirst := make(chan struct{})

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		start := time.Now()
		n := func() int {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, spawnEv{id: req.TaskID, start: start, spawnN: len(events) + 1})
			return len(events)
		}()
		if n == 1 {
			// First writer arrived: hold the tree (the session is
			// "running") until the test tells it to finish.
			<-releaseFirst
		}
		end := time.Now()
		mu.Lock()
		for i := range events {
			if events[i].id == req.TaskID {
				events[i].end = end
			}
		}
		mu.Unlock()
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(ctx, prd.ID, spawn, tlVerify) }()

	waitDeadline := time.Now().Add(15 * time.Second)
	for {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n == 2 {
			t.Fatal("second task spawned while the first task still holds the tree lock")
		}
		if n == 1 {
			break
		}
		select {
		case err := <-ran:
			t.Fatalf("Run exited before a second writer was gated: %v", err)
		case <-time.After(100 * time.Millisecond):
			if time.Now().After(waitDeadline) {
				t.Fatalf("Run did not reach the first spawn within %v (got %d events)", waitDeadline, n)
			}
		}
	}

	close(releaseFirst)
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not finish after the first task released the tree lock")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected both tasks to eventually spawn, got %d events", len(events))
	}
	first := events[0]
	second := events[1]
	if second.start.Before(first.end) {
		t.Errorf("serialized ordering violated: second spawn started %v before first finished",
			first.end.Sub(second.start))
	}
}

// TestExecutorTreeLockWaitReason verifies that while a task waits on the
// tree lock it is visible as waiting_capacity with a "tree lock" wait
// reason, acquires as soon as the holder releases, finishes, and leaves
// no lock file behind.
func TestExecutorTreeLockWaitReason(t *testing.T) {
	fastTreeLockPolling(t)
	projDir := t.TempDir()
	mgr := newTreeLockExecutorManager(t, Config{})
	st := mgr.Store()
	prd, taskID := makeTreePRD(t, mgr, projDir)

	// A live holder in the store keeps the lock so the test-owned
	// acquire is treated as a foreign (live) holder.
	holderPRD, err := st.CreatePRD("holder prd", projDir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD holder: %v", err)
	}
	if err := st.SetStories(holderPRD.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "Hold", Spec: "hold the tree"}},
	}}); err != nil {
		t.Fatalf("SetStories holder: %v", err)
	}
	holderPRD, ok := st.GetPRD(holderPRD.ID)
	if !ok {
		t.Fatalf("GetPRD holder")
	}
	holder := holderPRD.Story[0].Tasks[0]
	holder.Status = TaskInProgress
	if err := st.SaveTask(&holder); err != nil {
		t.Fatalf("SaveTask holder: %v", err)
	}

	key, root, err := ResolveTreeKey(prd, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	holderRelease, err := AcquireTreeLock(context.Background(), key, root, holder.ID, holderPRD.ID,
		func(id string) (TaskStatus, bool) {
			cur, ok := st.GetTask(id)
			if !ok {
				return "", false
			}
			return cur.Status, true
		},
		nil, nil, time.Second)
	if err != nil {
		t.Fatalf("holder AcquireTreeLock: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(ctx, prd.ID, okSpawn, tlVerify) }()

	// Poll for the visible wait state on the waiting task.
	sawWaiting := false
	deadline := time.Now().Add(10 * time.Second)
	for {
		cur, _ := st.TaskSnapshot(taskID)
		if cur.Status == TaskWaitingCapacity && strings.Contains(cur.WaitReason, "tree lock") {
			sawWaiting = true
			break
		}
		select {
		case err := <-ran:
			t.Fatalf("Run exited before entering the tree-lock wait: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("task never became visible waiting on the tree lock (status=%q reason=%q)",
				cur.Status, cur.WaitReason)
		}
	}
	_ = sawWaiting

	holderRelease()
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not finish after the holder released the tree lock")
	}

	cur, _ := st.GetTask(taskID)
	if cur.Status != TaskCompleted {
		t.Errorf("task status after run = %q; want completed", cur.Status)
	}
	path := lockPathFor(t, prd)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("lock file should be released after the run: %s (stat=%v)", path, statErr)
	}
}

// TestExecutorTreeLockReleasedOnSpawnFailure verifies the deferred
// release covers the spawn-error exit path: failed spawn, no lock file
// left, and a later run on the same tree starts cleanly.
func TestExecutorTreeLockReleasedOnSpawnFailure(t *testing.T) {
	fastTreeLockPolling(t)
	projDir := t.TempDir()
	// This test is about the lock on a spawn failure, so the spawn error must
	// end the task at once instead of being retried as an infrastructure fault.
	retryOff := false
	mgr := newTreeLockExecutorManager(t, Config{InfraRetry: config.InfraRetryConfig{Enabled: &retryOff}})
	st := mgr.Store()
	prd, taskID := makeTreePRD(t, mgr, projDir)

	spawnCalls := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCalls++
		return SpawnResult{}, errors.New("spawn boom")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := mgr.Run(ctx, prd.ID, spawn, tlVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawnCalls != 1 {
		t.Fatalf("spawn calls = %d; want 1", spawnCalls)
	}
	cur, _ := st.GetTask(taskID)
	if cur.Status != TaskFailed {
		t.Errorf("task status = %q; want failed", cur.Status)
	}
	path := lockPathFor(t, prd)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("lock file must be released on the spawn-failure path: %s (stat=%v)", path, statErr)
	}

	// A later PRD on the same tree must be able to start and finish.
	prd2, _ := makeTreePRD(t, mgr, projDir)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel2()
	if err := mgr.Run(ctx2, prd2.ID, okSpawn, tlVerify); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if _, statErr := os.Stat(lockPathFor(t, prd2)); !os.IsNotExist(statErr) {
		t.Errorf("second-run lock file should be released (stat=%v)", statErr)
	}
}

// TestExecutorTreeLockReleasedAfterVerify verifies the held lock survives
// to the end of a successful run (spawn+verify both execute under it)
// and is released afterwards.
func TestExecutorTreeLockReleasedAfterVerify(t *testing.T) {
	fastTreeLockPolling(t)
	projDir := t.TempDir()
	mgr := newTreeLockExecutorManager(t, Config{})
	st := mgr.Store()
	prd, taskID := makeTreePRD(t, mgr, projDir)

	verifyUnderLock := false
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		path := lockPathFor(t, prd)
		if _, statErr := os.Stat(path); statErr == nil {
			verifyUnderLock = true // held, as it must be during verify
		} else {
			t.Errorf("tree lock not held during verify: %v", statErr)
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := mgr.Run(ctx, prd.ID, okSpawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cur, _ := st.GetTask(taskID)
	if cur.Status != TaskCompleted {
		t.Errorf("task status = %q; want completed", cur.Status)
	}
	if !verifyUnderLock {
		t.Error("verify did not observe the tree lock held")
	}
	path := lockPathFor(t, prd)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("lock file must be released after a successful run: %s (stat=%v)", path, statErr)
	}
}

// TestExecutorTreeLockDirtyTreeRefused verifies refuse_dirty_tree_start
// hard-refuses admission on an uncommitted tree: the task fails before
// spawn, the gate message is visible on the task, and no lock file is
// written.
func TestExecutorTreeLockDirtyTreeRefused(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatalf("dirty file: %v", err)
	}
	mgr := newTreeLockExecutorManager(t, Config{RefuseDirtyTreeStart: true})
	st := mgr.Store()
	prd, taskID := makeTreePRD(t, mgr, repo)

	spawnCalls := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCalls++
		return SpawnResult{SessionID: "s"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := mgr.Run(ctx, prd.ID, spawn, tlVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawnCalls != 0 {
		t.Errorf("spawn called %d times; gate must refuse before spawn", spawnCalls)
	}
	cur, _ := st.GetTask(taskID)
	if cur.Status != TaskFailed {
		t.Errorf("task status = %q; want failed", cur.Status)
	}
	wantSubstrings := []string{
		"refuse_dirty_tree_start", // gate identity, not a retry-able failure
		"1 uncommitted entry",     // names the dirty entry count
		"scratch.txt",             // names the offending entry
		"commit, stash",           // actionable: commit or stash first
		"git clean",               // actionable: clean untracked
		"autonomous.refuse_dirty_tree_start: false", // actionable: the config escape hatch
	}
	got := strings.ToLower(cur.Error)
	for _, want := range wantSubstrings {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("task error %q should contain %q", cur.Error, want)
		}
	}
	path := lockPathFor(t, prd)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("no lock file may be written on a refused admission (stat=%v)", statErr)
	}
}

// TestExecutorTreeLockCleanTreeAdmitted verifies the gate passes a clean
// tree, letting the task run to completion.
func TestExecutorTreeLockCleanTreeAdmitted(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newTreeLockExecutorManager(t, Config{RefuseDirtyTreeStart: true})
	st := mgr.Store()
	prd, taskID := makeTreePRD(t, mgr, repo)

	spawnCalls := 0
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawnCalls++
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := mgr.Run(ctx, prd.ID, spawn, tlVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawnCalls != 1 {
		t.Errorf("spawn calls = %d; want 1", spawnCalls)
	}
	cur, _ := st.GetTask(taskID)
	t.Logf("clean-tree task status=%q error=%q", cur.Status, cur.Error)
	if cur.Status != TaskCompleted {
		t.Errorf("task status = %q; want completed", cur.Status)
	}
	path := lockPathFor(t, prd)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("lock file must be released after the run (stat=%v)", statErr)
	}
}

// TestExecutorTreeLockGateOffAllowsDirty verifies the gate is a no-op
// when refuse_dirty_tree_start is disabled (the default).
func TestExecutorTreeLockGateOffAllowsDirty(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatalf("dirty file: %v", err)
	}
	mgr := newTreeLockExecutorManager(t, Config{}) // gate off
	st := mgr.Store()
	prd, taskID := makeTreePRD(t, mgr, repo)

	spawnCalls := 0
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawnCalls++
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := mgr.Run(ctx, prd.ID, spawn, tlVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawnCalls != 1 {
		t.Errorf("spawn calls = %d; want 1", spawnCalls)
	}
	cur, _ := st.GetTask(taskID)
	if cur.Status != TaskCompleted {
		t.Errorf("task status = %q; want completed", cur.Status)
	}
}

// TestExecutorTreeLockWorktreeParallel verifies that two independent PRDs
// run in separate trees — a linked worktree vs. the main checkout (two
// distinct canonical keys) — run in parallel: both spawns arrive at the
// barrier before either is allowed to finish.
// TestExecutorTreeLockMissingTreeSkips pins the not-yet-created-tree
// path: a task whose canonical tree does not exist on disk (it may be
// created by the work itself, or is a test fake) must still be admitted
// — the gate and the lock can only reason about trees they can see, and
// refusing would wedge every tree-creating task hard before spawn.
func TestExecutorTreeLockMissingTreeSkips(t *testing.T) {
	fastTreeLockPolling(t)
	mgr := newTreeLockExecutorManager(t, Config{RefuseDirtyTreeStart: true})
	st := mgr.Store()
	prddir := filepath.Join(t.TempDir(), "no-such-dir")
	prd, taskID := makeTreePRD(t, mgr, prddir)
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-miss"}, nil
	}
	if err := mgr.Run(context.Background(), prd.ID, spawn, tlVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cur, _ := st.GetTask(taskID)
	if cur.Status != TaskCompleted {
		t.Fatalf("missing-tree task status=%q error=%q; want completed (spawn expected to proceed)", cur.Status, cur.Error)
	}
}

func TestExecutorTreeLockWorktreeParallel(t *testing.T) {
	fastTreeLockPolling(t)
	repo := newTestRepo(t)
	wt := addTestWorktree(t, repo, "wtpar")

	// Two managers (two daemons) over ONE shared repo — the realistic
	// cross-daemon deployment the tree lock exists for. Separate stores
	// keep this test from tripping a pre-existing manager-internal
	// persist race that has nothing to do with the lock; everything
	// shared between the two runs is the on-disk tree state, which is
	// exactly what the lock must arbitrate.
	mgrA := newTreeLockExecutorManager(t, Config{})
	mgrB := newTreeLockExecutorManager(t, Config{})

	prdA, _ := makeTreePRD(t, mgrA, repo) // direct mode in the main checkout
	prdB, _ := makeTreePRD(t, mgrB, wt)   // worktree mode
	prdA.Status = PRDApproved
	if err := mgrA.Store().SavePRD(prdA); err != nil {
		t.Fatalf("SavePRD A: %v", err)
	}
	prdB.Status = PRDApproved
	prdB.Git.Branch = "wt-wtpar" // worktree mode: PRD owns this worktree
	if err := mgrB.Store().SavePRD(prdB); err != nil {
		t.Fatalf("SavePRD B: %v", err)
	}
	// Sanity: the two PRDs must resolve to different tree keys, so the
	// test failure below can only mean the lock serialized them.
	keyA, _, err := ResolveTreeKey(prdA, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey A: %v", err)
	}
	keyB, _, err := ResolveTreeKey(prdB, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey B: %v", err)
	}
	if keyA == keyB {
		t.Fatalf("test setup: expected distinct tree keys, got %q for both", keyA)
	}

	// Barrier: both spawns must arrive while each other is still waiting.
	// If the trees were (wrongly) serialized, the first spawn can never
	// see the second one and fails its local deadline instead.
	var arrived sync.WaitGroup
	arrived.Add(2)
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		arrived.Done()
		bothIn := make(chan struct{})
		go func() { arrived.Wait(); close(bothIn) }()
		select {
		case <-bothIn:
		case <-time.After(6 * time.Second):
			return SpawnResult{}, errors.New("second tree's spawn never arrived — independent trees were serialized")
		case <-ctx.Done():
			return SpawnResult{}, ctx.Err()
		}
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	errA := make(chan error, 1)
	errB := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		errA <- mgrA.Run(ctx, prdA.ID, spawn, tlVerify)
	}()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		errB <- mgrB.Run(ctx, prdB.ID, spawn, tlVerify)
	}()
	var runErrs []string
	if err := <-errA; err != nil {
		runErrs = append(runErrs, "A: "+err.Error())
	}
	if err := <-errB; err != nil {
		runErrs = append(runErrs, "B: "+err.Error())
	}
	if len(runErrs) > 0 {
		t.Fatalf("parallel worktree PRDs did not both complete: %v", runErrs)
	}
	for _, chk := range []struct {
		id string
		st *Store
	}{{prdA.ID, mgrA.Store()}, {prdB.ID, mgrB.Store()}} {
		prd, _ := chk.st.GetPRD(chk.id)
		for _, s := range prd.Story {
			for _, tk := range s.Tasks {
				if tk.Status != TaskCompleted {
					t.Errorf("%s task %s status = %q; want completed", chk.id, tk.ID, tk.Status)
				}
			}
		}
	}
}
