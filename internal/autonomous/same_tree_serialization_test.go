// 2026-10-13 incident regression tests (docs/plans/2026-10-14-
// concurrency-and-budget-design.md §6(i); the 2026-10-17 supervisor
// rule "only one running PRD may hold a direct tree").
//
// Incident shape: THREE PRDs, ONE shared direct working tree, each with
// an in-flight write/release session. Observed damage: (a) one
// PRD's file showing up in another PRD's diff (clobber), and (b) two
// concurrent release tasks consuming the same version (v9.0.64), so
// one increment was never visible.
//
// These tests pin the daemon's post-incident property from the
// shared state itself (the commit log, the index, HEAD, and the
// version file):
//
//  1. NO TWO PRDs interleave. Three PRDs launched concurrently on one
//     direct tree (the shape of the real API: approvals pre-stored,
//     Manager.Run called per PRD): the daemon admits one and REFUSES
//     the rest at tree admission ("tree busy", the BL469 gate in
//     tree_admission.go) instead of interleaving them; the recorded
//     spawn sessions therefore never overlap.
//
//  2. No clobber. Every worker commit authored exactly its own file;
//     the refused PRDs never touched the tree at all (their files are
//     neither on disk, staged, nor in HEAD).
//
//  3. No lost version bump. The release critical section (read →
//     bump → write against the shared tree) runs only while the task
//     holds the tree: the two concurrent bump tasks are serialized
//     with the second visible in a "tree lock" wait episode, and each
//     lands its own increment — the version ends at the second
//     sequential bump from the base ("9.10.1"), not the single
//     stale-read bump the incident shipped.
//
// Two layers of the tree-lock wiring protect the tree, and these two
// tests pin BOTH: the PRD-level admission gate (BL469,
// tree_admission.go — "tree busy" refusal at Run start) and the
// task-level writer lock (BL428 §3a — the holdTree acquisition in
// executor.go, whose wait state is the visible "tree lock" episode).
// Comment out the gate and TestSameTreeThreePRDsSerialize goes red
// (all three PRDs execute on the shared tree); comment out holdTree
// and TestSameTreeConcurrentReleaseBumpsDoNotCollide goes red (bumps
// overlap, one increment is swallowed, no "tree lock" wait episode is
// recorded). Either removal is the 2026-10-13 clobber shape.
package autonomous

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// gitOutFn returns a git runner for dir that is safe to call from the
// executor's goroutines: it returns errors instead of t.Fatalf.
func gitOutFn(dir string) func(args ...string) (string, error) {
	return func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
}

// gitLogCommits splits `git log --name-only --format=@@%s` into
// map[subject]fileList. Main testing goroutine only.
func gitLogCommits(t *testing.T, repo string) map[string][]string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "log", "--name-only", "--format=@@%s").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	commits := map[string][]string{}
	for _, block := range strings.Split(string(out), "@@") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		subject, files, _ := strings.Cut(block, "\n")
		var list []string
		for _, ln := range strings.Split(files, "\n") {
			if ln = strings.TrimSpace(ln); ln != "" {
				list = append(list, ln)
			}
		}
		commits[strings.TrimSpace(subject)] = list
	}
	return commits
}

// gitHeadFiles returns the set of files in HEAD.
func gitHeadFiles(t *testing.T, repo string) map[string]bool {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "ls-tree", "-r", "--name-only", "HEAD").Output()
	if err != nil {
		t.Fatalf("git ls-tree: %v", err)
	}
	m := map[string]bool{}
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			m[ln] = true
		}
	}
	return m
}

// TestSameTreeThreePRDsSerialize is the incident replayed: three PRDs,
// one shared project_dir (a real git repo), each with one task; each
// task commits its own unique file. All three PRDs are launched
// concurrently — three goroutines calling Manager.Run, approvals
// pre-stored, the executor contract of the real production API — which
// is exactly how the incident's three PRDs were in flight at once.
//
// Assertions (from the recorded spawn intervals + the repo):
//
//  1. no two PRDs interleave — at least one of the concurrently
//     launched PRDs is refused by the tree-admission gate ("tree
//     busy") rather than interleaved, at least one PRD runs, and no
//     two recorded spawn sessions on the tree overlap;
//
//  2. no clobber — every worker commit authored ONLY its own file, and
//     the PRDs that ran see their own file in final HEAD;
//
//  3. the refused PRDs never touched the tree — their files are
//     neither on disk, staged, nor committed.
func TestSameTreeThreePRDsSerialize(t *testing.T) {
	fastTreeLockPolling(t)
	// Established-project repo: the scratch patterns datawatch seeds at
	// run start are pre-committed, so an admitted run does not mutate
	// the tree.
	repo := gitRepoWithDWSeed(t)
	mgr := newTreeLockExecutorManager(t, Config{})
	st := mgr.Store()

	type onePRD struct {
		prd   *PRD
		task  string
		file  string
		err   error
		intv  interval
		touched bool
	}
	ones := make([]*onePRD, 0, 3)
	files := map[string]*onePRD{}
	for i := 1; i <= 3; i++ {
		prd, task := makeTreePRD(t, mgr, repo)
		o := &onePRD{prd: prd, task: task, file: fmt.Sprintf("prd-%d.txt", i)}
		ones = append(ones, o)
		files[task] = o
	}

	var mu sync.Mutex
	spawnIntervals := map[string]interval{}
	gitRun := gitOutFn(repo)
	seam := SpawnFn(func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		o, ok := files[req.TaskID]
		if !ok || req.ProjectDir != repo {
			return SpawnResult{}, fmt.Errorf("test seam: unexpected task %q or dir %q", req.TaskID, req.ProjectDir)
		}
		start := time.Now()
		if err := os.WriteFile(filepath.Join(repo, o.file), []byte(o.file), 0o644); err != nil {
			return SpawnResult{}, fmt.Errorf("worker: write %s: %w", o.file, err)
		}
		if out, err := gitRun("add", o.file); err != nil {
			return SpawnResult{}, fmt.Errorf("worker: git add: %v\n%s", err, out)
		}
		if out, err := gitRun("commit", "-a", "-q", "-m", "worker: "+o.file); err != nil {
			return SpawnResult{}, fmt.Errorf("worker: git commit: %v\n%s", err, out)
		}
		end := time.Now()
		mu.Lock()
		spawnIntervals[req.TaskID] = interval{start, end}
		o.touched = true
		mu.Unlock()
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	})

	// Launch all three PRDs concurrently — the incident shape.
	var wg sync.WaitGroup
	for _, o := range ones {
		wg.Add(1)
		go func(o *onePRD) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			o.err = mgr.Run(ctx, o.prd.ID, seam, tlVerify)
		}(o)
	}
	wg.Wait()

	ran := 0
	refused := 0
	for _, o := range ones {
		if o.err != nil {
			refused++
			if !strings.Contains(o.err.Error(), "tree busy") {
				t.Errorf("admission failure for %s (file %s): %v; want the tree-admission refusal (\"tree busy\") — a concurrent same-tree PRD is refused, not interleaved",
					o.prd.ID, o.file, o.err)
			}
			if o.touched {
				t.Errorf("refused %s (file %s) nonetheless ran its worker on the tree — that is the incident", o.prd.ID, o.file)
			}
			continue
		}
		ran++
		o.intv = spawnIntervals[o.task]
		if !o.touched {
			t.Errorf("admitted %s: run completed without its worker touching the tree", o.prd.ID)
		}
		cur, ok := st.GetTask(o.task)
		if !ok || cur.Status != TaskCompleted {
			t.Errorf("admitted %s: task not completed (status=%q, error=%s)", o.prd.ID, cur.Status, cur.Error)
		}
	}
	if ran == 0 {
		var errs []error
		for _, o := range ones {
			errs = append(errs, o.err)
		}
		t.Fatalf("no PRD was admitted to the shared tree (errors: %v); want at least one admitted + the others refused", errs)
	}
	if refused == 0 {
		t.Errorf("all %d concurrently-launched PRDs ran on the one shared direct tree — the BL469 tree-admission refusal is gone (exactly the 2026-10-13 incident shape)", len(ones))
	}

	// 1) No two spawn sessions overlapped on the tree. (With the gate
	// in place at most one ran; the check pins the general property
	// regardless of how admission interleaves.)
	touched := make([]*onePRD, 0, len(ones))
	for _, o := range ones {
		if o.touched {
			touched = append(touched, o)
		}
	}
	sort.Slice(touched, func(i, j int) bool { return touched[i].intv.start.Before(touched[j].intv.start) })
	for i := 0; i < len(touched); i++ {
		for j := i + 1; j < len(touched); j++ {
			a, b := touched[i].intv, touched[j].intv
			if !a.end.Before(b.start) && !b.end.Before(a.start) {
				t.Errorf("two workers interleaved on the shared tree (the incident): %s vs %s",
					touched[i].prd.ID, touched[j].prd.ID)
			}
		}
	}

	// 2) No clobber, from the repo itself: every worker commit authored
	// exactly its own file and that file is in final HEAD.
	commits := gitLogCommits(t, repo)
	written := map[string]bool{}
	for subject, list := range commits {
		if !strings.HasPrefix(subject, "worker: ") {
			continue // seed commits
		}
		file := strings.TrimPrefix(subject, "worker: ")
		if len(list) != 1 || list[0] != file {
			t.Errorf("worker commit %q touched %v; want exactly [%s] — a foreign PRD's file inside a PRD's diff is the clobber signature", subject, list, file)
		}
		written[file] = true
	}
	head := gitHeadFiles(t, repo)
	for _, o := range ones {
		if o.err != nil {
			// 3) Refused PRDs never touched the tree at all.
			if out, _ := exec.Command("git", "-C", repo, "ls-files", "--others", "--exclude-standard", o.file).Output(); strings.TrimSpace(string(out)) != "" {
				t.Errorf("refused %s left its file %s untracked in the tree", o.prd.ID, o.file)
			}
			if out, _ := exec.Command("git", "-C", repo, "ls-files", "--cached", o.file).Output(); strings.TrimSpace(string(out)) != "" {
				t.Errorf("refused %s staged its file %s in the index", o.prd.ID, o.file)
			}
			if head[o.file] {
				t.Errorf("refused %s's file %s is in HEAD — it must never have been committed", o.prd.ID, o.file)
			}
			continue
		}
		if !written[o.file] {
			t.Errorf("admitted %s's file %s was never committed by itself (clobbered into another commit?)", o.prd.ID, o.file)
		}
		if !head[o.file] {
			t.Errorf("admitted %s's file %s missing from final HEAD", o.prd.ID, o.file)
		}
	}
}

type interval struct{ start, end time.Time }

// releaseBump mimics a release version bump: patch + 1 with a carry
// at 10 — the shape of the datawatch release version, so two
// sequential bumps from 9.9.9 land 9.10.0 → 9.10.1.
func releaseBump(v string) (string, error) {
	var a, b, c int
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d.%d.%d", &a, &b, &c); err != nil {
		return "", fmt.Errorf("cannot parse version %q: %w", v, err)
	}
	c++
	if c == 10 {
		c = 0
		b++
	}
	return fmt.Sprintf("%d.%d.%d", a, b, c), nil
}

// bumpsLanded counts the sequential bumps separating base from got
// (0 when equal; -1 when got is not reachable from base — a
// stale-read/corrupt write).
func bumpsLanded(base, got string) int {
	const capBumps = 200
	if got == base {
		return 0
	}
	v := base
	for n := 1; n <= capBumps; n++ {
		nv, err := releaseBump(v)
		if err != nil {
			return -1
		}
		if nv == got {
			return n
		}
		v = nv
	}
	return -1
}


// TestSameTreeConcurrentReleaseBumpsDoNotCollide pins the second
// branch of the 2026-10-13 incident — concurrent release-style
// read-bump-writes of a shared version (the incident: two concurrent
// release tasks consumed v9.0.64, so only one increment was visible in
// the shipped version). This branch pins the TASK-LEVEL writer lock
// (BL428 §3a, the holdTree acquisition in executor.go) that serializes
// concurrent tasks on the same tree: two concurrent release tasks in
// one PRD on one shared direct tree (MaxConcurrentTasks=2), each seam
// performing the release critical section — read the shared VERSION
// file (starting "9.9.9"), bump the patch by 1, write it back.
//
// (TestSameTreeThreePRDsSerialize pins the PRD-level tree-admission
// gate, the BL469 refusal; a single PRD's own tasks are exempt from
// their parent's admission, so the two tests together pin BOTH layers
// of the tree-lock wiring: remove either one and one of the two tests
// goes red.)
//
// Assertions:
//
//  1. Serialized — the two bump sessions never overlap; while the
//     first task holds the tree the second is visible in the task
//     store in a waiting state with a "tree lock" wait reason (the
//     recorded episode), and then acquires and finishes.
//
//  2. No lost bump — exactly two sequential bumps land, so the file
//     ends at "9.10.1" (9.9.9 → 9.10.0 → 9.10.1). With holdTree
//     commented out both tasks read the same stale value and
//     last-write-wins: the file keeps one bump and there is no
//     "tree lock" wait episode — red in both assertions.
func TestSameTreeConcurrentReleaseBumpsDoNotCollide(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	versionFile := filepath.Join(repo, "VERSION.txt")
	const base = "9.9.9"
	if err := os.WriteFile(versionFile, []byte(base+"\n"), 0o644); err != nil {
		t.Fatalf("seed VERSION.txt: %v", err)
	}
	mgr := newTreeLockExecutorManager(t, Config{})
	st := mgr.Store()

	prd, err := st.CreatePRD("release bumps", repo, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "release-a", Spec: "bump A"}, {Title: "release-b", Spec: "bump B"}},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD")
	}
	prd.Status = PRDApproved
	prd.MaxConcurrentTasks = 2
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskA := prd.Story[0].Tasks[0].ID
	taskB := prd.Story[0].Tasks[1].ID
	if taskA == "" || taskB == "" {
		t.Fatal("task IDs not assigned")
	}

	// The release critical section: read → bump → write, with a window
	// between read and write wide enough that WITHOUT the tree lock
	// both concurrent tasks read the same stale value (last write
	// wins, one increment swallowed — the incident).
	const readWriteGap = 40 * time.Millisecond
	var mu sync.Mutex
	window := map[string]interval{}
	seam := SpawnFn(func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		start := time.Now()
		cur, err := os.ReadFile(versionFile)
		if err != nil {
			return SpawnResult{}, err
		}
		next, err := releaseBump(string(cur))
		if err != nil {
			return SpawnResult{}, err
		}
		time.Sleep(readWriteGap)
		if err := os.WriteFile(versionFile, []byte(next+"\n"), 0o644); err != nil {
			return SpawnResult{}, err
		}
		mu.Lock()
		window[req.TaskID] = interval{start: start, end: time.Now()}
		mu.Unlock()
		return SpawnResult{SessionID: "sess-bump-" + req.TaskID}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(ctx, prd.ID, seam, tlVerify) }()

	// Record the second task's wait episode: while it waits for the
	// tree lock it must be visible as waiting with a "tree lock"
	// wait reason.
	episodeSeen := false
	runDone := false
	var runErr error
	deadline := time.Now().Add(15 * time.Second)
	for !runDone {
		ca, _ := st.TaskSnapshot(taskA)
		cb, _ := st.TaskSnapshot(taskB)
		if (ca.Status == TaskWaitingCapacity && strings.Contains(ca.WaitReason, "tree lock")) ||
			(cb.Status == TaskWaitingCapacity && strings.Contains(cb.WaitReason, "tree lock")) {
			episodeSeen = true
		}
		select {
		case runErr = <-ran:
			runDone = true
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("Run did not complete the two concurrent release tasks")
		}
	}
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}

	ta, _ := st.GetTask(taskA)
	tb, _ := st.GetTask(taskB)
	if ta.Status != TaskCompleted || tb.Status != TaskCompleted {
		t.Fatalf("release tasks not completed (a=%q b=%q, aerr=%s berr=%s)", ta.Status, tb.Status, ta.Error, tb.Error)
	}
	if !episodeSeen {
		t.Errorf("neither task was ever visible waiting with a \"tree lock\" wait reason — the second release task was not serialized behind the tree lock (the holdTree wiring is gone; the incident's concurrent bump shape)")
	}

	ia, iaOK := window[taskA]
	ib, ibOK := window[taskB]
	if !iaOK || !ibOK {
		t.Fatalf("expected both release tasks to run the bump once each; observed %d bump session(s)", len(window))
	}
	if !ia.end.Before(ib.start) && !ib.end.Before(ia.start) {
		t.Errorf("the two bump sessions overlapped on the shared tree (the incident's swallowed-increment shape): A and B each did read-bump-write concurrently")
	}

	// No lost bump: exactly two sequential bumps land, and in either
	// order the file ends at the second sequential bump from the base.
	var b []byte
	got := ""
	if b, err = os.ReadFile(versionFile); err != nil {
		t.Fatalf("read VERSION.txt: %v", err)
	}
	got = strings.TrimSpace(string(b))
	if bumpsLanded(base, got) != 2 {
		t.Errorf("version after two release bumps is %q (%d sequential bump(s) from %q); want exactly two — the incident's failure was concurrent tasks consuming the same version and one increment being swallowed",
			got, bumpsLanded(base, got), base)
	}
	if got != "9.10.1" {
		t.Errorf("version is %q; want \"9.10.1\" (9.9.9 → 9.10.0 → 9.10.1, both bumps landed)", got)
	}
}
