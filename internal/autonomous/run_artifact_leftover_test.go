package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newScratchProjectRepo makes proj a minimal git repository (so the
// cleanup's tracked/untracked distinction is real).
func newScratchProjectRepo(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, rerr := exec.Command(git, append([]string{"-C", dir}, args...)...).CombinedOutput(); rerr != nil {
			t.Fatalf("git %v: %v: %s", args, rerr, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "runner")
	mustWriteFile(t, filepath.Join(dir, "keep.txt"), "baseline\n")
	run("add", "--", "keep.txt")
	run("commit", "-q", "-m", "baseline")
	return dir
}

// TestLeftoverWarningsSurfacedOnRollup covers the end-of-run cleanup
// hook: when the PRD is rolled up to a terminal status, untracked
// harness scratch in the project directory is removed, and unexpected
// untracked files land in PRD.LeftoverWarnings via the store.
func TestLeftoverWarningsSurfacedOnRollup(t *testing.T) {
	m, prd := bl382RunningPRD(t)

	proj := newScratchProjectRepo(t)
	mustWriteFile(t, filepath.Join(proj, ".decompose-output-abc123.json"), "{}")
	mustWriteFile(t, filepath.Join(proj, "mystery.txt"), "unexpected\n")

	// Bring every task to a terminal state so the rollup can commit.
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Story[0].Tasks[0].Status = TaskCompleted
	prd.Story[0].Tasks[1].Status = TaskCompleted
	prd.ProjectDir = proj
	_ = m.Store().SavePRD(prd)

	observed := PRDRunning
	rev, _ := m.store.PRDRev(prd.ID)
	old, ok := m.commitRollupStatus(prd, observed, rev, PRDFailed, "failed")
	if !ok {
		t.Fatalf("commitRollupStatus: ok = false, want true (old=%q)", old)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !containsWarning(got.LeftoverWarnings, "leftover file: mystery.txt") {
		t.Errorf("LeftoverWarnings = %v, want it to surface the unexpected file", got.LeftoverWarnings)
	}
	if n := len(got.LeftoverWarnings); n != 1 {
		t.Errorf("LeftoverWarnings = %v, want exactly one warning", got.LeftoverWarnings)
	}
	if _, err := os.Stat(filepath.Join(proj, ".decompose-output-abc123.json")); !os.IsNotExist(err) {
		t.Errorf("untracked scratch was not removed (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "mystery.txt")); err != nil {
		t.Errorf("unexpected file must be left in place: %v", err)
	}
}

// TestLeftoverWarningsDedupedOnRepeatedCleanup checks that finishing
// cleanup twice for the same PRD does not duplicate the warnings.
func TestLeftoverWarningsDedupedOnRepeatedCleanup(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	proj := t.TempDir()
	// No git repo here: cleanup must report the scratch candidate as a
	// leftover instead of deleting it.
	mustWriteFile(t, filepath.Join(proj, ".decompose-output-dedup.json"), "{}")

	m.finishRunArtifacts(prd.ID, proj)
	m.finishRunArtifacts(prd.ID, proj)

	got, _ := m.Store().GetPRD(prd.ID)
	if n := len(got.LeftoverWarnings); n != 1 {
		t.Errorf("LeftoverWarnings = %v, want exactly one (deduped)", got.LeftoverWarnings)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func containsWarning(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
