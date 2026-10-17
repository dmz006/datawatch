package autonomous

// Same-tree regression tests for BL423/BL426/BL428 §3a: three PRDs
// whose tasks all write one shared project tree must serialize their
// writes, and the concurrent release version bumps they make must not
// drop increments.
//
// Shape under test — the incident shape (three PRDs, one tree) as the
// daemon supports it today: one manager, three PRDs on one shared
// direct-mode checkout, all three Run concurrently. BL469 (admitTree)
// refuses a PRD while another RUNNING PRD holds the same tree and
// admits it against its own ancestors — so the three PRDs below form
// one genealogy chain (root, child, grandchild), the supported
// multi-PRD shape for a shared tree in one daemon. Their three tasks
// still contend for the SAME canonical tree, and once all three Runs
// are admitted the only mechanism that serializes them is the per-task
// tree lock under test (executeOne → holdTree →
// m.acquireTreeWritersLock, BL428 §3a): admitTree ran once per Run,
// before the contention began, and the capacity ledger is off
// (Config{}).
//
// These tests exercise the cross-PRD form that TestExecutorTreeLock*
// (two tasks of ONE approved PRD) and TestAdmitTree* (admission
// only) cover only partially: the lock is what keeps three
// independently admitted runs from interleaving their writes.
//
// Red/green: with the holdTree wiring in executeOne removed, the three
// spawns overlap (test 1's interval, waiting-episode and commit
// assertions fail) and all three bumps read the same version (test
// 2's final value is 9.9.10 — a lost increment — instead of 9.9.12).
//
// Note: task state is read with Store.TaskSnapshot (value copy under
// the store lock) — the sanctioned concurrent-read API. Under -race,
// these tests can surface pre-existing executor write-side races (e.g.
// direct t.SessionID writes outside the store lock); those are
// production bugs in the write path, not in this test, and predate
// it (TestExecutorTreeLockSerializesSameTree hits the same).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type runErrSameTree struct {
	prd string
	err error
}

// spawnSpan records the exclusive window a task's spawn held its claim
// on the shared tree (start: entered spawn; end: spawn finished,
// including its commit). Serialized PRDs must have pairwise-disjoint
// spans.
type spawnSpan struct {
	taskID string
	start  time.Time
	end    time.Time
}

// treeTaskSnap is one poll of a task's status + wait reason.
type treeTaskSnap struct {
	status TaskStatus
	reason string
}

// startTreeTaskSnapper polls (status, wait-reason) for every task ID at
// ~1 ms until stop() is called, then returns the samples per task. Used
// to observe the waiting episode BL428 §3a promises: a task blocked on a
// shared tree shows status waiting_capacity with a "tree lock: ..."
// reason. The wait reason is one-shot (cleared at the admit that
// follows), so it must be sampled while it lasts.
func startTreeTaskSnapper(st *Store, taskIDs []string) func() map[string][]treeTaskSnap {
	mu := sync.Mutex{}
	samples := map[string][]treeTaskSnap{}
	stopping := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopping:
				return
			case <-ticker.C:
				for _, id := range taskIDs {
					// TaskSnapshot: value copy under the store lock — a value-copy
					// read safe against the executor's concurrent MutateTask status
					// writes (GetTask's shared pointer is race-unsafe mid-run).
					cur, ok := st.TaskSnapshot(id)
					if !ok {
						continue
					}
					mu.Lock()
					samples[id] = append(samples[id], treeTaskSnap{status: cur.Status, reason: cur.WaitReason})
					mu.Unlock()
				}
			}
		}
	}()
	return func() map[string][]treeTaskSnap {
		close(stopping)
		<-done
		mu.Lock()
		defer mu.Unlock()
		out := map[string][]treeTaskSnap{}
		for k, v := range samples {
			out[k] = append([]treeTaskSnap(nil), v...)
		}
		return out
	}
}

// runPRDsConcurrently launches one Run goroutine per PRD, in order,
// staggering the launches by 10 ms — far smaller than the workers'
// ~60 ms holds, so all three runs are in flight at the same time — to
// keep the BL469 admit ordering deterministic (a PRD is admitted only
// against its ancestors, which must already be running; the chain is
// admitted ancestor-first). All three runs then contend for the same
// tree lock. Fatals on any Run error (a tree-busy refusal here would
// mean the BL469 admit chain itself is broken).
func runPRDsConcurrently(t *testing.T, mgr *Manager, prdIDs []string, spawns []SpawnFn, verify VerifyFn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	errs := make(chan runErrSameTree, len(prdIDs))
	for i := range prdIDs {
		idx, sp := i, spawns[i]
		if i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		go func() {
			errs <- runErrSameTree{prd: prdIDs[idx], err: mgr.Run(ctx, prdIDs[idx], sp, verify)}
		}()
	}
	msgs := []string{}
	for i := 0; i < len(prdIDs); i++ {
		r := <-errs
		if r.err != nil {
			msgs = append(msgs, fmt.Sprintf("PRD %s: %v", r.prd, r.err))
		}
	}
	if len(msgs) > 0 {
		t.Fatalf("Runs failed: %s", strings.Join(msgs, "; "))
	}
}

// newSameTreePRD creates one direct-mode PRD on the shared checkout
// with a single story/task, approved and ready to Run. parentID/depth
// record the genealogy chain (the form BL469 admits against a shared
// tree in one daemon). Mirrors makeTreePRD plus CreatePRDWithParent.
func newSameTreePRD(t *testing.T, st *Store, projectDir, parentID string, depth int) (*PRD, string) {
	t.Helper()
	prd, err := st.CreatePRDWithParent("same-tree serialize", projectDir, "claude-code", "", EffortNormal, parentID, "", depth)
	if err != nil {
		t.Fatalf("CreatePRDWithParent: %v", err)
	}
	if err := st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "write the tree"}},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD")
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

// writingSpawn returns a SpawnFn that behaves like the incident's
// workers: it does work (60 ms — wide enough to overlap its siblings
// when the tree lock is not held), writes its own file into the shared
// repo, then commits everything dirty (`git add -A`, the way a real
// session commits) under its own message.
func writingSpawn(repo, ownFile, commitMsg string, spans *[]spawnSpan, spansMu *sync.Mutex) SpawnFn {
	return func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		start := time.Now()
		time.Sleep(60 * time.Millisecond) // worker work held against the shared tree
		finish := func() {
			spansMu.Lock()
			*spans = append(*spans, spawnSpan{taskID: req.TaskID, start: start, end: time.Now()})
			spansMu.Unlock()
		}
		defer finish()
		if err := os.WriteFile(filepath.Join(repo, ownFile), []byte("work done by task "+req.TaskID+"\n"), 0o644); err != nil {
			return SpawnResult{}, fmt.Errorf("write %s: %w", ownFile, err)
		}
		if out, err := exec.CommandContext(ctx, "git", "-C", repo, "add", "-A").CombinedOutput(); err != nil {
			return SpawnResult{}, fmt.Errorf("git add -A: %v: %s", err, out)
		}
		if out, err := exec.CommandContext(ctx, "git", "-C", repo, "commit", "-q", "-m", commitMsg).CombinedOutput(); err != nil {
			return SpawnResult{}, fmt.Errorf("git commit: %v: %s", err, out)
		}
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
}

func TestSameTreeThreePRDsSerialize(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	baseOut, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	base := strings.TrimSpace(string(baseOut))

	mgr := newTreeLockExecutorManager(t, Config{})
	st := mgr.Store()

	// One genealogy chain on the shared tree: the multi-PRD shape BL469
	// admits against one direct checkout in a single daemon. All three
	// PRDs point at the same repo, so all three tasks resolve the SAME
	// tree key; their writers may interleave only if the tree lock under
	// test is disabled.
	prd1, task1 := newSameTreePRD(t, st, repo, "", 0)
	prd2, task2 := newSameTreePRD(t, st, repo, prd1.ID, 1)
	prd3, task3 := newSameTreePRD(t, st, repo, prd2.ID, 2)

	stopSnooping := startTreeTaskSnapper(st, []string{task1, task2, task3})

	var spansMu sync.Mutex
	var spans []spawnSpan
	spawn1 := writingSpawn(repo, "prd-1.txt", "prd-1: commit own file", &spans, &spansMu)
	spawn2 := writingSpawn(repo, "prd-2.txt", "prd-2: commit own file", &spans, &spansMu)
	spawn3 := writingSpawn(repo, "prd-3.txt", "prd-3: commit own file", &spans, &spansMu)

	runPRDsConcurrently(t, mgr, []string{prd1.ID, prd2.ID, prd3.ID}, []SpawnFn{spawn1, spawn2, spawn3}, tlVerify)
	snaps := stopSnooping()

	spansMu.Lock()
	got := append([]spawnSpan(nil), spans...)
	spansMu.Unlock()
	if len(got) != 3 {
		t.Fatalf("expected 3 spawns, got %d: %+v", len(got), got)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].start.Before(got[j].start) })

	// (1) Serialize: no two spawn windows overlap.
	for i := 1; i < len(got); i++ {
		if got[i].start.Before(got[i-1].end) {
			t.Errorf("task %s's spawn overlapped task %s's (started %s, previous ended %s) — writers on the same tree interleaved",
				got[i].taskID, got[i-1].taskID, got[i].start.Format("15:04:05.000000"), got[i-1].end.Format("15:04:05.000000"))
		}
	}

	// (2) Waiting episode: the PRD whose spawn started last must have
	// shown a waiting episode blocked on the tree lock.
	last := got[len(got)-1].taskID
	var waitSaw bool
	for _, s := range snaps[last] {
		if s.status == TaskWaitingCapacity && strings.Contains(s.reason, "tree lock") {
			waitSaw = true
			break
		}
	}
	if !waitSaw {
		t.Errorf("last-started task %s never observed in waiting state with a tree-lock reason; samples: %+v", last, snaps[last])
	}

	// (3) Commit hygiene: exactly three commits on top of base, each
	// touching only its own file (a `git add -A` while a sibling's work
	// is uncommitted drags it in), and all three files in HEAD.
	logOut, err := exec.Command("git", "-C", repo, "log", base+"..HEAD", "--name-only", "--format=@@@%s").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	type commitBlock struct {
		msg   string
		files []string
	}
	var blocks []commitBlock
	var cur *commitBlock
	flush := func() {
		if cur != nil {
			blocks = append(blocks, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(logOut)), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "@@") {
			flush()
			c := commitBlock{msg: strings.TrimPrefix(line, "@@@")}
			cur = &c
			continue
		}
		if line == "" || cur == nil {
			continue
		}
		cur.files = append(cur.files, line)
	}
	flush()
	if len(blocks) != 3 {
		t.Fatalf("expected exactly 3 commits on top of base, got %d: %+v", len(blocks), blocks)
	}
	wantFileByMsg := map[string]string{
		"prd-1: commit own file": "prd-1.txt",
		"prd-2: commit own file": "prd-2.txt",
		"prd-3: commit own file": "prd-3.txt",
	}
	for _, b := range blocks {
		want, known := wantFileByMsg[b.msg]
		if !known {
			t.Errorf("unexpected commit %q (files %v)", b.msg, b.files)
			continue
		}
		if len(b.files) != 1 || b.files[0] != want {
			t.Errorf("commit %q touched %v; want exactly [%s] — sibling work leaked into the commit", b.msg, b.files, want)
		}
	}
	lsOut, err := exec.Command("git", "-C", repo, "ls-tree", "--name-only", "HEAD").Output()
	if err != nil {
		t.Fatalf("git ls-tree: %v", err)
	}
	tracked := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(lsOut)), "\n") {
		tracked[line] = true
	}
	for _, f := range []string{"prd-1.txt", "prd-2.txt", "prd-3.txt"} {
		if !tracked[f] {
			t.Errorf("HEAD does not track %s\n%s", f, lsOut)
		}
	}
}

func TestSameTreeConcurrentReleaseBumpsDoNotCollide(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newTreeLockExecutorManager(t, Config{})
	st := mgr.Store()

	// Task IDs are not needed by this test: the bump bookkeeping is keyed
	// by SpawnRequest.TaskID inside the seams.
	prd1, _ := newSameTreePRD(t, st, repo, "", 0)
	prd2, _ := newSameTreePRD(t, st, repo, prd1.ID, 1)
	prd3, _ := newSameTreePRD(t, st, repo, prd2.ID, 2)

	// The shared "version" a release task bumps: a plain value with a
	// wide read-then-write window (40 ms of work between read and write).
	// The tree lock — the only thing all three PRDs hold in common — is
	// what must serialize the read-modify-write; the value itself has
	// no locking of its own.
	var version atomic.Value
	version.Store("9.9.9")
	type bump struct{ read, write string }
	var bumpMu sync.Mutex
	bumps := map[string]bump{}
	bumpSpawn := func() SpawnFn {
		return func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
			read := version.Load().(string)
			time.Sleep(40 * time.Millisecond) // widen the race window between read and write
			dots := strings.Split(read, ".")
			if len(dots) != 3 {
				return SpawnResult{}, fmt.Errorf("bad version %q", read)
			}
			patch, err := strconv.Atoi(dots[2])
			if err != nil {
				return SpawnResult{}, fmt.Errorf("bad patch %q: %v", dots[2], err)
			}
			writeVal := dots[0] + "." + dots[1] + "." + strconv.Itoa(patch+1)
			version.Store(writeVal)
			bumpMu.Lock()
			bumps[req.TaskID] = bump{read: read, write: writeVal}
			bumpMu.Unlock()
			return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
		}
	}

	runPRDsConcurrently(t, mgr, []string{prd1.ID, prd2.ID, prd3.ID}, []SpawnFn{bumpSpawn(), bumpSpawn(), bumpSpawn()}, tlVerify)

	bumpMu.Lock()
	defer bumpMu.Unlock()
	final := version.Load().(string)
	seen := map[string]int{}
	for id, b := range bumps {
		seen[b.write]++
		t.Logf("task %s: read %s, wrote %s", id, b.read, b.write)
	}
	for _, v := range []string{"9.9.10", "9.9.11", "9.9.12"} {
		if seen[v] != 1 {
			t.Errorf("writes = %v; want each of 9.9.10/9.9.11/9.9.12 exactly once (three sequential increments)", seen)
			break
		}
	}
	if final != "9.9.12" {
		t.Errorf("final version = %q; want 9.9.12 (exactly three sequential increments from 9.9.9; anything else is a lost increment)", final)
	}
}
