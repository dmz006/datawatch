package tooling

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRunRepo makes dir a minimal git repository so the
// tracked/untracked distinction exercised by the tests is real.
func gitRunRepo(t *testing.T, dir string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", dir}, args...)
		if out, rerr := exec.Command(git, full...).CombinedOutput(); rerr != nil {
			t.Fatalf("git %v: %v: %s", args, rerr, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "runner")
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func hasPath(list []string, want string) bool {
	for _, p := range list {
		if p == want {
			return true
		}
	}
	return false
}

// TestRunArtifactUntrackedRemoved: an untracked harness scratch file
// matching a known pattern is removed.
func TestRunArtifactUntrackedRemoved(t *testing.T) {
	dir := t.TempDir()
	gitRunRepo(t, dir)
	scratch := filepath.Join(dir, ".decompose-output-abc123.json")
	writeFileT(t, scratch, "{}")

	removed, leftover := CleanupRunArtifacts(dir)
	if !hasPath(removed, ".decompose-output-abc123.json") {
		t.Errorf("removed = %v, want .decompose-output-abc123.json", removed)
	}
	if hasPath(leftover, ".decompose-output-abc123.json") {
		t.Errorf("leftover = %v, must not contain the removed scratch file", leftover)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Errorf("scratch file still exists (err=%v)", err)
	}
}

// TestRunArtifactTrackedNotRemoved: a TRACKED file with a scratch
// pattern name is never deleted and surfaces as a leftover.
func TestRunArtifactTrackedNotRemoved(t *testing.T) {
	dir := t.TempDir()
	gitRunRepo(t, dir)
	rel := "decompose-output.json"
	writeFileT(t, filepath.Join(dir, rel), "{}")
	git, _ := exec.LookPath("git")
	if git == "" {
		t.Skip("git not on PATH")
	}
	full := append([]string{"-C", dir}, "add", "--", rel)
	if out, err := exec.Command(git, full...).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}

	removed, leftover := CleanupRunArtifacts(dir)
	if hasPath(removed, rel) {
		t.Errorf("removed = %v, must not contain tracked file %s", removed, rel)
	}
	if !hasPath(leftover, rel) {
		t.Errorf("leftover = %v, want tracked file %s reported", leftover, rel)
	}
	if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
		t.Errorf("tracked file was deleted: %v", err)
	}
}

// TestRunArtifactUnknownLeftover: an unknown untracked file is not
// removed and appears in the leftover list.
func TestRunArtifactUnknownLeftover(t *testing.T) {
	dir := t.TempDir()
	gitRunRepo(t, dir)
	writeFileT(t, filepath.Join(dir, "mystery.txt"), "unexpected\n")

	removed, leftover := CleanupRunArtifacts(dir)
	if hasPath(removed, "mystery.txt") {
		t.Errorf("removed = %v, must not contain mystery.txt", removed)
	}
	if !hasPath(leftover, "mystery.txt") {
		t.Errorf("leftover = %v, want mystery.txt reported", leftover)
	}
	if _, err := os.Stat(filepath.Join(dir, "mystery.txt")); err != nil {
		t.Errorf("unknown file was deleted: %v", err)
	}
}

// TestRunArtifactOutsideProjectDirUntouched: a scratch-pattern file
// outside projectDir is never touched.
func TestRunArtifactOutsideProjectDirUntouched(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRunRepo(t, dir)
	outside := filepath.Join(root, ".decompose-output-outside.json")
	writeFileT(t, outside, "{}")

	removed, leftover := CleanupRunArtifacts(dir)
	for _, list := range [][]string{removed, leftover} {
		for _, p := range list {
			t.Logf("cleanup reported %q", p)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("file outside projectDir was touched: %v", err)
	}
	for _, p := range removed {
		if filepath.IsAbs(p) || strings.HasPrefix(p, "..") {
			t.Errorf("cleanup reported a path outside projectDir: %q", p)
		}
	}
}

// TestRunArtifactNoGitFallback: when the project directory is not in a
// work tree (git unavailable for deletion), scratch candidates are
// reported as leftovers instead of deleted.
func TestRunArtifactNoGitFallback(t *testing.T) {
	dir := t.TempDir()
	rel := ".decompose-output-fallback.json"
	writeFileT(t, filepath.Join(dir, rel), "{}")

	removed, leftover := CleanupRunArtifacts(dir)
	if hasPath(removed, rel) {
		t.Errorf("removed = %v, must not delete without git confirmation", removed)
	}
	if !hasPath(leftover, rel) {
		t.Errorf("leftover = %v, want %s reported", leftover, rel)
	}
	if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
		t.Errorf("candidate file was deleted without git: %v", err)
	}
}
