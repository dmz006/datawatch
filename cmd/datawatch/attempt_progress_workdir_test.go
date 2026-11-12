package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireGit skips the test when git is not on PATH.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// runTestGit runs git with a fixed identity and fails the test on non-zero
// exit. It always returns stdout.
func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.name=datawatch-test",
		"-c", "user.email=datawatch-test@localhost",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

// newGitRepo creates a fresh git repository in t.TempDir() with one tracked
// file ("hello") committed, so HEAD exists.
func newGitRepo(t *testing.T) (dir string, tracked string) {
	t.Helper()
	dir = t.TempDir()
	runTestGit(t, dir, "init", "-q")
	tracked = filepath.Join(dir, "hello")
	if err := os.WriteFile(tracked, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "hello")
	runTestGit(t, dir, "commit", "-q", "-m", "init")
	return dir, tracked
}

func TestWorkDirFingerprint_TrackedEditChangesFingerprint(t *testing.T) {
	requireGit(t)
	dir, tracked := newGitRepo(t)
	fp1, _ := workDirFingerprint(dir)
	// A clean tree has no status entries, so the baseline may be empty; the
	// assertion is that editing a tracked file produces a different one.
	if err := os.WriteFile(tracked, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp2, _ := workDirFingerprint(dir)
	if fp2 == "" {
		t.Fatalf("fingerprint after editing a tracked file = empty, want non-empty")
	}
	if fp2 == fp1 {
		t.Fatalf("fingerprint unchanged after editing a tracked file: %s", fp1)
	}
}

func TestWorkDirFingerprint_UntrackedFileChangesFingerprint(t *testing.T) {
	requireGit(t)
	dir, _ := newGitRepo(t)
	fp1, _ := workDirFingerprint(dir)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp2, _ := workDirFingerprint(dir)
	if fp2 == "" {
		t.Fatalf("fingerprint after creating an untracked file = empty, want non-empty")
	}
	if fp2 == fp1 {
		t.Fatalf("fingerprint unchanged after creating an untracked file: %s", fp1)
	}
}

func TestWorkDirFingerprint_EditSameDirtyFileTwice(t *testing.T) {
	requireGit(t)
	dir, tracked := newGitRepo(t)
	workDirFingerprint(dir) // baseline, unused (ignoring both returns)

	if err := os.WriteFile(tracked, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp1, _ := workDirFingerprint(dir)
	if fp1 == "" {
		t.Fatalf("fingerprint after first edit = empty, want non-empty")
	}

	// Small settle so the mtime (nanoseconds) can also differ; the size
	// difference below alone already changes the entry.
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(tracked, []byte("aa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp2, _ := workDirFingerprint(dir)
	if fp2 == "" {
		t.Fatalf("fingerprint after second edit = empty, want non-empty")
	}
	if fp2 == fp1 {
		t.Fatalf("fingerprint did not change when re-editing an already-dirty file: %s", fp1)
	}
}

func TestWorkDirFingerprint_HarnessScratchFilesIgnored(t *testing.T) {
	requireGit(t)
	dir, _ := newGitRepo(t)
	fp1, _ := workDirFingerprint(dir)

	changes := func(t *testing.T, mutate func()) {
		t.Helper()
		mutate()
		fp2, _ := workDirFingerprint(dir)
		if fp2 != fp1 {
			t.Fatalf("fingerprint changed (%q -> %q) after only touching harness scratch files", fp1, fp2)
		}
	}

	changes(t, func() {
		if err := os.WriteFile(filepath.Join(dir, "CHECKPOINT.md"), []byte("# check\none more line\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	changes(t, func() {
		if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "sessions"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".datawatch", "sessions", "x.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".datawatch", "state"), []byte("v"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	changes(t, func() {
		if err := os.WriteFile(filepath.Join(dir, ".decompose-output-abcd1234.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWorkDirFingerprint_NewCommitChangesHead(t *testing.T) {
	requireGit(t)
	dir, tracked := newGitRepo(t)
	if err := os.WriteFile(tracked, []byte("hello v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "hello")
	runTestGit(t, dir, "commit", "-q", "-m", "second")

	_, head1 := workDirFingerprint(dir)
	if head1 == "" {
		t.Fatalf("head after commits = empty, want a commit sha")
	}
	wantSha := strings.TrimSpace(runTestGit(t, dir, "rev-parse", "HEAD"))
	if head1 != wantSha {
		t.Fatalf("head = %q, want %q", head1, wantSha)
	}

	// Commit again; head must change.
	if err := os.WriteFile(tracked, []byte("hello v3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", "hello")
	runTestGit(t, dir, "commit", "-q", "-m", "third")
	_, head2 := workDirFingerprint(dir)
	wantSha2 := strings.TrimSpace(runTestGit(t, dir, "rev-parse", "HEAD"))
	if head2 != wantSha2 {
		t.Fatalf("head after second commit = %q, want %q", head2, wantSha2)
	}
	if head2 == head1 {
		t.Fatalf("head did not change after a new commit: %s", head1)
	}
}

func TestWorkDirFingerprint_NonGitFallbackWalk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp1, head := workDirFingerprint(dir)
	if head != "" {
		t.Fatalf("head for a non-git directory = %q, want empty", head)
	}
	if fp1 == "" {
		t.Fatalf("walk fingerprint for a non-git directory = empty, want non-empty")
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one, edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp2, _ := workDirFingerprint(dir)
	if fp2 == fp1 {
		t.Fatalf("walk fingerprint unchanged after editing a file: %s", fp1)
	}

	// Harness scratch files must be ignored by the walk fallback too.
	changes := func(t *testing.T, mutate func()) {
		t.Helper()
		base := fp2
		mutate()
		got, _ := workDirFingerprint(dir)
		if got != base {
			t.Fatalf("walk fingerprint changed (%q -> %q) after only touching harness scratch files", base, got)
		}
	}
	changes(t, func() {
		if err := os.WriteFile(filepath.Join(dir, "CHECKPOINT.md"), []byte("# cp\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	changes(t, func() {
		if err := os.MkdirAll(filepath.Join(dir, ".datawatch"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".datawatch", "y"), []byte("z"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
}

// TestWorkDirFingerprint_GitNotARepoError does not panic and returns an
// empty head for a plain directory (the fallback path).
func TestWorkDirFingerprint_EmptyWorkDir(t *testing.T) {
	fp, head := workDirFingerprint("")
	if fp != "" || head != "" {
		t.Fatalf("workDirFingerprint(\"\") = (%q, %q), want (\"\", \"\")", fp, head)
	}
}
