// Tests for the two dirty-tree probes (the refuse_dirty_tree_start gate in
// tree_lock.go and the await-external treeNotClean probe in
// await_external.go) when a tree's only change to a tracked file is the
// daemon's own marked blocks: the probes must ignore it, and a real edit in
// the same tree must still be seen.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dtWriteCommit writes rel under repo and commits it, so the tree is clean
// before the test dirties it.
func dtWriteCommit(t *testing.T, repo, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, rel), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	gitRun(t, repo, "add", rel)
	gitRun(t, repo, "commit", "-q", "-m", "seed "+rel)
}

// dtRealEditOutsideDaemonSections is CLAUDE.md with an operator edit in the
// base content plus the daemon's marked blocks appended — a mixed tree a
// dirty-tree probe must refuse. (Not a const: string slicing is not a
// constant expression.)
var dtRealEditOutsideDaemonSections = dsoBase + "\nExtra operator line.\n" + dsoDaemon[len(dsoBase):]

// TestDirtyTreeDaemonSectionStartGuardNotRefused verifies the
// refuse_dirty_tree_start gate admits a tree whose only uncommitted change
// is the daemon's own marked blocks on a tracked file: the task spawns and
// completes instead of failing on the gate.
func TestDirtyTreeDaemonSectionStartGuardNotRefused(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	dtWriteCommit(t, repo, "CLAUDE.md", dsoBase)
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatalf("dirty CLAUDE.md: %v", err)
	}
	if got, _ := runGitIn(repo, "status", "--porcelain"); !strings.Contains(got, "CLAUDE.md") {
		t.Fatalf("fixture is not dirty: %q", got)
	}
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
		t.Errorf("spawn calls = %d; want 1 (daemon-section-only tree must be admitted, not refused)", spawnCalls)
	}
	cur, _ := st.GetTask(taskID)
	if cur.Status != TaskCompleted {
		t.Errorf("task status = %q error=%q; want completed", cur.Status, cur.Error)
	}
}

// TestDirtyTreeDaemonSectionStartGuardRefusesRealEdit verifies a real edit
// alongside the daemon's marked blocks still trips the gate: the task fails
// before spawn with the unchanged refuse_dirty_tree_start message.
func TestDirtyTreeDaemonSectionStartGuardRefusesRealEdit(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	dtWriteCommit(t, repo, "CLAUDE.md", dsoBase)
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(dtRealEditOutsideDaemonSections), 0o644); err != nil {
		t.Fatalf("dirty CLAUDE.md: %v", err)
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
	got := strings.ToLower(cur.Error)
	for _, want := range []string{
		"refuse_dirty_tree_start", // gate identity
		"CLAUDE.md",               // names the offending entry
		"commit, stash",           // unchanged actionable message
	} {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("task error %q should contain %q", cur.Error, want)
		}
	}
}

// TestDirtyTreeDaemonSectionAwaitProbeClean verifies the await-external
// dirty probe (treeNotClean) reports a tree with only a daemon-section
// change on a tracked file as clean.
func TestDirtyTreeDaemonSectionAwaitProbeClean(t *testing.T) {
	repo := gitRepoWithDWSeed(t)
	dtWriteCommit(t, repo, "CLAUDE.md", dsoBase)
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatalf("dirty CLAUDE.md: %v", err)
	}
	if got, _ := runGitIn(repo, "status", "--porcelain"); !strings.Contains(got, "CLAUDE.md") {
		t.Fatalf("fixture is not dirty: %q", got)
	}
	m := newTreeLockExecutorManager(t, Config{})
	if m.treeNotClean(context.Background(), repo) {
		t.Errorf("treeNotClean = true; daemon-section-only change must read as clean")
	}
}

// TestDirtyTreeDaemonSectionAwaitProbeDirty verifies the same probe still
// reports a tree with a real edit as dirty.
func TestDirtyTreeDaemonSectionAwaitProbeDirty(t *testing.T) {
	repo := gitRepoWithDWSeed(t)
	dtWriteCommit(t, repo, "CLAUDE.md", dsoBase)
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(dtRealEditOutsideDaemonSections), 0o644); err != nil {
		t.Fatalf("dirty CLAUDE.md: %v", err)
	}
	m := newTreeLockExecutorManager(t, Config{})
	if !m.treeNotClean(context.Background(), repo) {
		t.Errorf("treeNotClean = false; real edit must read as dirty")
	}
}
