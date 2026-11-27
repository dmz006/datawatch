package tooling

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func artifactGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.test"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func artifactWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A project folder can hold other repositories (clones, linked work trees,
// submodules). What one of them tracks is tracked, although the project's own
// repository knows nothing about it: cleanup must never delete it.
func TestCleanupRunArtifactsKeepsFilesTrackedByNestedRepository(t *testing.T) {
	project := t.TempDir()
	artifactGit(t, project, "init", "-q")
	nested := filepath.Join(project, "vendored", "clone")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactGit(t, nested, "init", "-q")
	tracked := filepath.Join(nested, "fixtures", "decompose-output.json")
	artifactWrite(t, tracked)
	artifactGit(t, nested, "add", "fixtures/decompose-output.json")
	artifactGit(t, nested, "commit", "-qm", "fixture")
	// Real run scratch, untracked everywhere, is still removed: at the top
	// and inside the nested repository.
	top := filepath.Join(project, ".verify-output-abc.json")
	artifactWrite(t, top)
	inner := filepath.Join(nested, ".decompose-output-abc.json")
	artifactWrite(t, inner)

	removed, leftover := CleanupRunArtifacts(project)

	if _, err := os.Stat(tracked); err != nil {
		t.Errorf("a file the nested repository tracks was deleted: %v (removed=%v)", err, removed)
	}
	for _, p := range []string{top, inner} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("untracked run scratch %s was not removed (err=%v)", p, err)
		}
	}
	found := false
	for _, l := range leftover {
		if l == filepath.Join("vendored", "clone", "fixtures", "decompose-output.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("the kept tracked file is not reported as a leftover: %v", leftover)
	}
}

// The project's own repository can track a file in a folder that later became a
// repository of its own (git init there). The new inner repository does not
// know the file, but it is still tracked by the project: cleanup must keep it.
func TestCleanupRunArtifactsKeepsFileTheProjectTracksUnderALaterRepository(t *testing.T) {
	project := t.TempDir()
	artifactGit(t, project, "init", "-q")
	rel := filepath.Join("sub", "decompose-output.json")
	tracked := filepath.Join(project, rel)
	artifactWrite(t, tracked)
	artifactGit(t, project, "add", "sub/decompose-output.json")
	artifactGit(t, project, "commit", "-qm", "fixture")
	artifactGit(t, filepath.Join(project, "sub"), "init", "-q")

	removed, leftover := CleanupRunArtifacts(project)

	if _, err := os.Stat(tracked); err != nil {
		t.Fatalf("a file the project's repository tracks was deleted: %v (removed=%v)", err, removed)
	}
	found := false
	for _, l := range leftover {
		if l == rel {
			found = true
		}
	}
	if !found {
		t.Errorf("the kept tracked file is not reported as a leftover: %v", leftover)
	}
}

// When git cannot say whether a file is tracked, it is kept.
func TestCleanupRunArtifactsKeepsFileWhenGitCannotAnswer(t *testing.T) {
	project := t.TempDir()
	artifactGit(t, project, "init", "-q")
	broken := filepath.Join(project, "broken")
	file := filepath.Join(broken, "decompose-output.json")
	artifactWrite(t, file)
	// A work tree pointer to a repository that is not there: every git
	// command in that folder fails.
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("gitdir: ./no-such-repository\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, leftover := CleanupRunArtifacts(project)

	if _, err := os.Stat(file); err != nil {
		t.Fatalf("a file git could not judge was deleted: %v (removed=%v leftover=%v)", err, removed, leftover)
	}
}

// A nested repository's settings can name a command in core.fsmonitor, which
// git runs when it reads that repository's index. Asking whether a file there
// is tracked must not run it.
func TestCleanupRunArtifactsDoesNotRunNestedRepositoryFsmonitor(t *testing.T) {
	project := t.TempDir()
	artifactGit(t, project, "init", "-q")
	nested := filepath.Join(project, "vendored", "clone")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactGit(t, nested, "init", "-q")
	tracked := filepath.Join(nested, "decompose-output.json")
	artifactWrite(t, tracked)
	artifactGit(t, nested, "add", "decompose-output.json")
	artifactGit(t, nested, "commit", "-qm", "fixture")
	scratch := filepath.Join(nested, ".verify-output-abc.json")
	artifactWrite(t, scratch)

	outside := t.TempDir()
	marker := filepath.Join(outside, "fsmonitor-ran")
	hook := filepath.Join(outside, "fsmonitor.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	artifactGit(t, nested, "config", "core.fsmonitor", hook)
	// Control: without an override this git does run the command.
	artifactGit(t, nested, "ls-files")
	if err := os.Remove(marker); err != nil {
		t.Fatalf("control: plain git did not run the fsmonitor command: %v", err)
	}

	removed, _ := CleanupRunArtifacts(project)

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("cleanup ran the nested repository's fsmonitor command (stat err=%v)", err)
	}
	if _, err := os.Stat(tracked); err != nil {
		t.Errorf("a file the nested repository tracks was deleted: %v (removed=%v)", err, removed)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Errorf("untracked run scratch was not removed (err=%v)", err)
	}
}
