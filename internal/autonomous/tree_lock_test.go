package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Mirrors the datawatch repo, whose .gitignore already covers
	// .datawatch/: a project that adopts the lock gitignores it.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".datawatch/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "t@datawatch")
	gitRun(t, dir, "config", "user.name", "test")
	gitRun(t, dir, "commit", "--allow-empty", "-q", "-m", "base")
	gitRun(t, dir, "add", ".gitignore")
	gitRun(t, dir, "commit", "-q", "-m", "gitignore")
	return dir
}

func addTestWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), name)
	gitRun(t, repo, "worktree", "add", wt, "-b", "wt-"+name)
	return wt
}

func TestTreeKeyMainCheckout(t *testing.T) {
	repo := newTestRepo(t)
	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	// Direct mode in the main checkout: key per base checkout, treeRoot
	// is the actual write root.
	if want := "tree:" + repo; key != want {
		t.Errorf("key = %q; want %q", key, want)
	}
	if root != repo {
		t.Errorf("treeRoot = %q; want %q", root, repo)
	}
}

func TestTreeKeyLinkedWorktreeResolvesToBase(t *testing.T) {
	repo := newTestRepo(t)
	wt := addTestWorktree(t, repo, "wt")

	// Direct mode (no PRD branch) in a linked worktree: the key is the
	// SHARED BASE checkout (so a second holder on the base collides),
	// but the treeRoot stays the linked worktree — that is where writes
	// and the drift check operate.
	key, root, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: wt}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	if want := "tree:" + repo; key != want {
		t.Errorf("key = %q; want shared base %q", key, want)
	}
	if root != wt {
		t.Errorf("treeRoot = %q; want the linked worktree %q", root, wt)
	}
	// The advisory lock file therefore lives in the main checkout for
	// both holders — one file to inspect.
	if got := treeLockPath(key, root); got != filepath.Join(repo, ".datawatch", "tree.lock") {
		t.Errorf("lock path = %q; want main-checkout lock", got)
	}
}

func TestTreeKeySubdirectoryResolvesToSharedBase(t *testing.T) {
	repo := newTestRepo(t)
	sub := filepath.Join(repo, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	keySub, rootSub, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: sub}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey(sub): %v", err)
	}
	keyRepo, _, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey(repo): %v", err)
	}
	want := "tree:" + repo
	if keySub != want {
		t.Errorf("subdir key = %q; want %q", keySub, want)
	}
	if keySub != keyRepo {
		t.Errorf("subdir key = %q; checkout key = %q; want equal", keySub, keyRepo)
	}
	// treeRoot stays the actual write root (the subdirectory), not the base.
	if rootSub != sub {
		t.Errorf("subdir treeRoot = %q; want %q", rootSub, sub)
	}
	// The lock file still lands on the shared base, discoverable from the
	// main checkout.
	if got := treeLockPath(keySub, rootSub); got != filepath.Join(repo, ".datawatch", "tree.lock") {
		t.Errorf("subdir lock path = %q; want shared base lock", got)
	}
}

func TestTreeKeyWorktreeModePerWorktree(t *testing.T) {
	repo := newTestRepo(t)
	wt := addTestWorktree(t, repo, "wt")

	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: wt, Git: GitWorkflow{Branch: "automaton/a1"}}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	// Worktree mode keys per worktree — NOT the shared base.
	want := "tree:" + wt
	if key != want {
		t.Errorf("key = %q; want %q", key, want)
	}
	if key == "tree:"+repo {
		t.Errorf("worktree-mode key collapsed to shared base %q", key)
	}
	if root != wt {
		t.Errorf("treeRoot = %q; want %q", root, wt)
	}

	// A per-task override back to the base repo is a different tree.
	keyOverride, rootOverride, err := ResolveTreeKey(
		&PRD{ID: "a1", ProjectDir: wt, Git: GitWorkflow{Branch: "automaton/a1"}},
		&Task{ID: "t1", ProjectDir: repo})
	if err != nil {
		t.Fatalf("task override: %v", err)
	}
	if keyOverride != "tree:"+repo || rootOverride != repo {
		t.Errorf("override key/root = %q/%q; want %q/%q", keyOverride, rootOverride, "tree:"+repo, repo)
	}
}

func TestTreeKeyTwoWorktreesDiffer(t *testing.T) {
	repo := newTestRepo(t)
	wt1 := addTestWorktree(t, repo, "wt-1")
	wt2 := addTestWorktree(t, repo, "wt-2")

	k1, _, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: wt1, Git: GitWorkflow{Branch: "automaton/a1"}}, nil)
	if err != nil {
		t.Fatalf("prd1: %v", err)
	}
	k2, _, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: wt2, Git: GitWorkflow{Branch: "automaton/a2"}}, nil)
	if err != nil {
		t.Fatalf("prd2: %v", err)
	}
	if k1 == k2 {
		t.Errorf("two worktree-mode PRDs resolved to one key %q; want distinct", k1)
	}
	if k1 != "tree:"+wt1 || k2 != "tree:"+wt2 {
		t.Errorf("keys = %q / %q; want %q / %q", k1, k2, "tree:"+wt1, "tree:"+wt2)
	}
}

func TestTreeKeyNonGitFallsBackToPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plain", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir = absClean(dir)

	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: dir}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	if want := "tree:" + dir; key != want {
		t.Errorf("key = %q; want %q", key, want)
	}
	if root != dir {
		t.Errorf("treeRoot = %q; want %q", root, dir)
	}

	// A sibling plain dir must be a distinct key.
	other := absClean(filepath.Join(t.TempDir(), "other-proj"))
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	key2, _, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: other}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey other: %v", err)
	}
	if key == key2 {
		t.Errorf("two plain dirs resolved to one key %q; want distinct", key)
	}

	// The non-git lock path is per write root, inside that dir's own
	// .datawatch.
	if got := treeLockPath(key, root); got != filepath.Join(dir, ".datawatch", "tree.lock") {
		t.Errorf("non-git lock path = %q; want %q", got, filepath.Join(dir, ".datawatch", "tree.lock"))
	}
}

func TestTreeKeyNoProjectDirErrors(t *testing.T) {
	if _, _, err := ResolveTreeKey(&PRD{ID: "a1"}, nil); err == nil {
		t.Error("empty PRD.ProjectDir: expected error")
	}
	if _, _, err := ResolveTreeKey(nil, nil); err == nil {
		t.Error("nil prd/task: expected error")
	}
}

func TestTreeLockPathMainCheckout(t *testing.T) {
	repo := newTestRepo(t)
	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	want := filepath.Join(repo, ".datawatch", "tree.lock")
	if got := treeLockPath(key, root); got != want {
		t.Errorf("treeLockPath = %q; want %q", got, want)
	}
}

func TestTreeLockPathWorktree(t *testing.T) {
	wt := absClean(filepath.Join(t.TempDir(), "prd-ab12cd34"))
	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: wt, Git: GitWorkflow{Branch: "automaton/a1"}}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	want := filepath.Join(wt, ".datawatch", "tree.lock")
	if got := treeLockPath(key, root); got != want {
		t.Errorf("treeLockPath = %q; want %q", got, want)
	}
}

func TestTreeLockPathSharedBaseOneFile(t *testing.T) {
	repo := newTestRepo(t)
	wt := addTestWorktree(t, repo, "wt")

	k1, r1, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("main checkout: %v", err)
	}
	k2, r2, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: wt}, nil)
	if err != nil {
		t.Fatalf("linked worktree: %v", err)
	}
	// Both holders of the shared base must agree on ONE advisory file,
	// discoverable in the main checkout.
	want := filepath.Join(repo, ".datawatch", "tree.lock")
	if got := treeLockPath(k1, r1); got != want {
		t.Errorf("main checkout lock = %q; want %q", got, want)
	}
	if got := treeLockPath(k2, r2); got != want {
		t.Errorf("linked worktree lock = %q; want %q (same shared file)", got, want)
	}
}

// ── Advisory acquisition machinery tests ──

func testTreeKey(t *testing.T, dir string) string {
	t.Helper()
	return "tree:" + absClean(dir)
}

func testLockPath(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(absClean(dir), ".datawatch", "tree.lock")
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustAcquireTreeLock(t *testing.T, key, root, holder, prd string, taskStatus func(string) (TaskStatus, bool), waitHook func(string)) func() {
	t.Helper()
	rel, err := AcquireTreeLock(context.Background(), key, root, holder, prd, taskStatus, waitHook, nil, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("AcquireTreeLock(%s as %s): %v", key, holder, err)
	}
	return rel
}

// t1LiveHook reports t1 as in-progress (live) and everyone else as
// unknown — used by tests where a second holder must WAIT on t1 rather
// than reclaim it.
func t1LiveHook(id string) (TaskStatus, bool) {
	if id == "t1" {
		return TaskInProgress, true
	}
	return "", false
}

func TestTreeLockBlocksSecondHolderThenReleases(t *testing.T) {
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := testLockPath(t, dir)

	rel1 := mustAcquireTreeLock(t, key, dir, "t1", "a1", nil, nil)

	waitHooks := make(chan string, 16)
	second := make(chan error, 1)
	go func() {
		rel, err := AcquireTreeLock(context.Background(), key, dir, "t2", "a2", t1LiveHook, func(r string) {
			waitHooks <- r
		}, nil, 100*time.Millisecond)
		if err == nil {
			rel()
		}
		second <- err
	}()

	// While t1 holds, t2 must be parked in the wait loop — not have
	// stolen the lock.
	time.Sleep(400 * time.Millisecond)
	select {
	case <-second:
		t.Fatal("second holder acquired while t1 still held")
	default:
	}

	rec, err := readLockRecord(lockPath)
	if err != nil {
		t.Fatalf("read held lock: %v", err)
	}
	if rec.HolderTaskID != "t1" {
		t.Fatalf("lock held by %q; want t1", rec.HolderTaskID)
	}

	// Release — the second holder must now proceed.
	rel1()
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("second holder failed after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second holder did not acquire within 5s after release")
	}

	// The wait hook received a reason naming the holder.
	select {
	case reason := <-waitHooks:
		if !strings.Contains(reason, "t1") {
			t.Errorf("wait hook reason %q does not name holder t1", reason)
		}
	default:
		t.Error("waitHook was never called for the blocked second holder")
	}
}

func TestTreeLockIdempotentReacquire(t *testing.T) {
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := testLockPath(t, dir)

	rel1 := mustAcquireTreeLock(t, key, dir, "t1", "a1", nil, nil)

	before, err := readLockRecord(lockPath)
	if err != nil || before.HolderTaskID != "t1" {
		t.Fatalf("initial lock record = %+v (err=%v)", before, err)
	}

	// Same holder re-acquires: idempotent, no error, no double-claim.
	rel2 := mustAcquireTreeLock(t, key, dir, "t1", "a1", nil, nil)

	// A DIFFERENT holder is still excluded while either handle is live
	// (t1 reported in-progress — so nothing is reclaimed; t2 parks in
	// the wait loop until its ctx cancels).
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if _, err := AcquireTreeLock(ctx, key, dir, "t2", "a2", t1LiveHook, nil, nil, 50*time.Millisecond); err == nil {
		t.Error("foreign holder acquired while t1 holds; want block until ctx cancels")
	}

	// Release the first handle — lock is gone…
	rel1()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock still present after release: %v", err)
	}
	// …and releasing the second (idempotent) handle is a silent no-op.
	rel2()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("second release should be a no-op, lock reappeared: %v", err)
	}
}

func TestTreeLockStaleUnknownHolderReclaimed(t *testing.T) {
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := testLockPath(t, dir)

	// A ghost holder: a lock record exists but no such task in the store.
	if err := writeLockRecord(lockPath, LockRecord{HolderTaskID: "ghost-task", HolderPRDID: "zzz", AcquiredAt: time.Now().Unix(), PID: 99999}); err != nil {
		t.Fatal(err)
	}

	var waitHookCalls []string
	// The unknown holder's lock is stale: reclaim WITHOUT waiting.
	rel := mustAcquireTreeLock(t, key, dir, "t1", "a1", func(id string) (TaskStatus, bool) {
		return "", false // no such task
	}, func(r string) { waitHookCalls = append(waitHookCalls, r) })

	if len(waitHookCalls) != 0 {
		t.Fatalf("waitHook called %d times; stale reclaim should not wait: %v", len(waitHookCalls), waitHookCalls)
	}
	rec, err := readLockRecord(lockPath)
	if err != nil {
		t.Fatalf("reclaimed lock unreadable: %v", err)
	}
	if rec.HolderTaskID != "t1" {
		t.Fatalf("lock held by %q after reclaim; want t1", rec.HolderTaskID)
	}
	rel()
}

func TestTreeLockStaleTerminalHolderReclaimed(t *testing.T) {
	statuses := map[string]TaskStatus{
		"failed":    TaskFailed,
		"cancelled": TaskCancelled,
		"completed": TaskCompleted,
	}
	for name, st := range statuses {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			key := testTreeKey(t, dir)
			lockPath := testLockPath(t, dir)

			owner := "dead-holder"
			if err := writeLockRecord(lockPath, LockRecord{HolderTaskID: owner, AcquiredAt: time.Now().Unix()}); err != nil {
				t.Fatal(err)
			}

			var waitHookCalls int
			rel := mustAcquireTreeLock(t, key, dir, "t1", "a1", func(id string) (TaskStatus, bool) {
				if id == owner {
					return st, true
				}
				return "", false
			}, func(r string) { waitHookCalls++ })

			if waitHookCalls != 0 {
				t.Fatalf("waitHook called %d times; terminal holder must be reclaimed without waiting", waitHookCalls)
			}
			rec, _ := readLockRecord(lockPath)
			if rec.HolderTaskID != "t1" {
				t.Fatalf("lock held by %q after reclaim; want t1", rec.HolderTaskID)
			}
			rel()
		})
	}
}

func TestTreeLockLiveHolderWaitsNotReclaimed(t *testing.T) {
	// A non-terminal (in-progress) holder is LIVE — the second acquirer
	// must WAIT on it, never reclaim it.
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := testLockPath(t, dir)
	owner := "t-live"
	if err := writeLockRecord(lockPath, LockRecord{HolderTaskID: owner, AcquiredAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}

	var waitHookCalls int
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	_, err := AcquireTreeLock(ctx, key, dir, "t2", "a2", func(id string) (TaskStatus, bool) {
		return TaskInProgress, true
	}, func(r string) { waitHookCalls++ }, nil, 80*time.Millisecond)
	if err == nil {
		t.Fatal("acquired while a live holder held the lock")
	}
	if waitHookCalls == 0 {
		t.Error("waitHook never called while waiting on a live holder")
	}
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("a live holder's lock was deleted by the waiter: %v", statErr)
	}
}

func TestTreeLockDoubleReleaseNoop(t *testing.T) {
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := testLockPath(t, dir)

	rel := mustAcquireTreeLock(t, key, dir, "t1", "a1", nil, nil)
	rel()
	rel() // must be a silent no-op — no error, no panic
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock present after double release: %v", err)
	}
}

func TestTreeLockReleaseGuardRefusesForeignHolder(t *testing.T) {
	// After a reclaim race a successor holds the lock. A DIFFERENT
	// holder's release handle must NOT delete the successor's fresh lock.
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := treeLockPath(key, dir)

	relSelf := mustAcquireTreeLock(t, key, dir, "t1", "a1", nil, nil)
	_ = relSelf

	newTreeLockRelease(lockPath, "t2")() // foreign holder release

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("foreign holder's release deleted the live lock: %v", err)
	}
}

func TestTreeLockCancelDuringWait(t *testing.T) {
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := testLockPath(t, dir)

	rel1 := mustAcquireTreeLock(t, key, dir, "t1", "a1", nil, nil)
	defer rel1()

	// isCancelled-flip path: t2 waits, gets cancelled mid-wait-loop.
	deadline := time.Now().Add(150 * time.Millisecond)
	var waitHookCalls int
	_, err := AcquireTreeLock(context.Background(), key, dir, "t2", "a2", t1LiveHook, func(r string) {
		waitHookCalls++
	}, func() bool {
		return time.Now().After(deadline)
	}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("acquire should have been cancelled while waiting (isCancelled)")
	}
	if waitHookCalls < 1 {
		t.Error("cancelled waiter never called waitHook — it was not in the wait loop")
	}
	// t1's lock is untouched and still readable.
	rec, rerr := readLockRecord(lockPath)
	if rerr != nil || rec.HolderTaskID != "t1" {
		t.Fatalf("t1's lock disturbed by cancelling waiter: %+v (err=%v)", rec, rerr)
	}

	// ctx-cancel path: same guarantee.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := AcquireTreeLock(ctx, key, dir, "t3", "a3", t1LiveHook, nil, nil, 100*time.Millisecond); err == nil {
		t.Fatal("acquire should have been cancelled via ctx while waiting")
	}
	if rec, rerr := readLockRecord(lockPath); rerr != nil || rec.HolderTaskID != "t1" {
		t.Fatalf("t1's lock disturbed by ctx-cancelled waiter: %+v (err=%v)", rec, rerr)
	}
}

func TestTreeLockStaleGraceProtectsFreshUnreadableRecord(t *testing.T) {
	// A record that cannot be parsed (empty holder) is protected by the
	// 30s grace window after creation: a waiter must WAIT on it, not
	// reclaim it — the holder may still be inside its create→write
	// window. Same record with an old mtime is stale-reclaimed.
	dir := t.TempDir()
	key := testTreeKey(t, dir)
	lockPath := testLockPath(t, dir)
	mustMkdirAll(t, filepath.Dir(lockPath))
	if err := os.WriteFile(lockPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Fresh (just-created) unreadable record → wait, then ctx cancels.
	var waitCalls int
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := AcquireTreeLock(ctx, key, dir, "t2", "a2", t1LiveHook, func(string) { waitCalls++ }, nil, 50*time.Millisecond)
	if err == nil {
		t.Fatal("reclaimed a FRESH unreadable lock inside the grace window")
	}
	if waitCalls == 0 {
		t.Error("waiter on a fresh unreadable lock never ran the wait hook")
	}
	if _, serr := os.Stat(lockPath); serr != nil {
		t.Fatalf("fresh unreadable lock deleted during grace: %v", serr)
	}

	// Age the record past the grace → stale-reclaim proceeds.
	past := time.Now().Add(-40 * time.Second)
	if err := os.Chtimes(lockPath, past, past); err != nil {
		t.Fatal(err)
	}
	rel := mustAcquireTreeLock(t, key, dir, "t2", "a2", nil, nil)
	rec, rerr := readLockRecord(lockPath)
	if rerr != nil || rec.HolderTaskID != "t2" {
		t.Fatalf("aged unreadable lock was not reclaimed: %+v (err=%v)", rec, rerr)
	}
	rel()
}

func TestTreeLockFileDoesNotDirtyGitStatus(t *testing.T) {
	// The advisory lock sits at a git-ignored / git-invisible location:
	// holding it must never show up in `git status --porcelain`.
	repo := newTestRepo(t)

	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	rel := mustAcquireTreeLock(t, key, root, "t1", "a1", nil, nil)

	out, err := exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Fatalf("held tree lock dirties git status:\n%s", s)
	}

	rel()
	out, err = exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status after release: %v\n%s", err, out)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Fatalf("git status dirty after release:\n%s", s)
	}
}

// A waiter that judged a lock stale from an earlier read must not delete
// the fresh lock a faster waiter has since claimed.
func TestRemoveStaleLock_RefusesSuccessorsLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tree.lock")
	stale := LockRecord{HolderTaskID: "old", AcquiredAt: 100, PID: 1}
	fresh := LockRecord{HolderTaskID: "new", AcquiredAt: 200, PID: 1}
	if err := writeLockRecord(path, fresh); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleLock(path, stale, true); err == nil {
		t.Fatal("removeStaleLock deleted a lock that changed hands")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fresh lock was removed: %v", err)
	}
	if err := removeStaleLock(path, fresh, true); err != nil {
		t.Fatalf("matching record should be removable: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock still present: %v", err)
	}
}
