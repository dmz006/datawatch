package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uwIgnoredSetup writes a .gitignore with .datawatch/, commits it, and creates
// an ignored evidence file under .datawatch/reports.
func uwIgnoredSetup(t *testing.T) string {
	t.Helper()
	dir := uwRepo(t)
	uwWrite(t, dir, ".gitignore", ".datawatch/\n")
	uwGit(t, dir, "add", ".gitignore")
	uwGit(t, dir, "commit", "-q", "-m", "add gitignore")
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	uwWrite(t, dir, ".datawatch/reports/evidence.txt", "evidence\n")
	return dir
}

// uwIgnoredVerifySetup is uwIgnoredSetup plus a worker-created verify script
// under the ignored .datawatch/verify directory.
func uwIgnoredVerifySetup(t *testing.T) string {
	t.Helper()
	dir := uwIgnoredSetup(t)
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "verify"), 0o755); err != nil {
		t.Fatal(err)
	}
	uwWrite(t, dir, ".datawatch/verify/example.sh", "#!/bin/sh\nexit 0\n")
	return dir
}

func TestTaskCommitIgnoredPathCommitsNormalFile(t *testing.T) {
	dir := uwIgnoredSetup(t)
	uwWrite(t, dir, "normal.txt", "normal change\n")
	before := uwGit(t, dir, "rev-parse", "HEAD")

	_, err := DefaultTaskCommit(context.Background(), dir,
		[]string{"normal.txt", ".datawatch/reports/evidence.txt"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	after := uwGit(t, dir, "rev-parse", "HEAD")
	if after == before {
		t.Fatal("expected a new commit")
	}
	names := uwGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"normal.txt", ".datawatch/reports/evidence.txt"} {
		if !strings.Contains(names, want) {
			t.Errorf("commit missing %s (a listed ignored path is force-staged now): %q", want, names)
		}
	}
}

func TestTaskCommitIgnoredOnlyCommitsIgnoredFile(t *testing.T) {
	dir := uwIgnoredSetup(t)
	before := uwGit(t, dir, "rev-parse", "HEAD")

	_, err := DefaultTaskCommit(context.Background(), dir,
		[]string{".datawatch/reports/evidence.txt"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); after == before {
		t.Fatal("expected a new commit of the force-staged ignored file")
	}
	names := uwGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(names, ".datawatch/reports/evidence.txt") {
		t.Errorf("commit missing the ignored file: %q", names)
	}
}

func TestTaskCommitIgnoredVerifyScript(t *testing.T) {
	dir := uwIgnoredVerifySetup(t)
	uwWrite(t, dir, "normal.txt", "normal change\n")
	before := uwGit(t, dir, "rev-parse", "HEAD")

	_, err := DefaultTaskCommit(context.Background(), dir,
		[]string{"normal.txt", ".datawatch/verify/example.sh"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); after == before {
		t.Fatal("expected a new commit")
	}
	names := uwGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"normal.txt", ".datawatch/verify/example.sh"} {
		if !strings.Contains(names, want) {
			t.Errorf("commit missing %s: %q", want, names)
		}
	}
}

// TestUncommittedHoldCommitFailureCarriesGitOutput pins the failure path: the
// real git commit fails with output and an exit status, and both survive into
// the hold reason and the held question text.
func TestUncommittedHoldCommitFailureCarriesGitOutput(t *testing.T) {
	dir := uwIgnoredVerifySetup(t)

	// Make the real `git commit` fail with output: git finds no identity,
	// because the identity environment is unset and the global and system
	// config files do not exist. `git add -f` needs no identity, so the
	// force staging still runs and the commit is what fails.
	for _, v := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if err := os.Unsetenv(v); err != nil {
			t.Fatalf("unset %s: %v", v, err)
		}
	}
	missing := filepath.Join(t.TempDir(), "absent-gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", missing)
	t.Setenv("GIT_CONFIG_SYSTEM", missing)

	_, err := DefaultTaskCommit(context.Background(), dir,
		[]string{".datawatch/verify/example.sh"}, "task commit")
	if err == nil {
		t.Fatal("DefaultTaskCommit: expected the commit to fail without an identity")
	}
	msg := err.Error()
	if !strings.Contains(msg, "git commit") {
		t.Errorf("error lacks the failed command: %q", msg)
	}
	if !strings.Contains(msg, "exit status") {
		t.Errorf("error lacks the exit status: %q", msg)
	}

	r := newUncommittedHoldRig(t, true)
	files := []UncommittedFile{{Path: ".datawatch/verify/example.sh", Status: "??"}}
	if herr := r.m.holdForUncommittedWork(r.prd, r.task, files, uncommittedCommitCause(err)); herr != nil {
		t.Fatalf("holdForUncommittedWork: %v", herr)
	}
	tk, ok := r.m.Store().GetTask(r.task.ID)
	if !ok {
		t.Fatal("GetTask returned nothing")
	}
	if tk.UncommittedWork == nil || tk.UncommittedWork.Reason == "" {
		t.Fatalf("hold = %+v", tk.UncommittedWork)
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want 1", len(qs))
	}
	q := qs[0]
	for _, want := range []string{"exit status", "unable to auto-detect email address"} {
		if !strings.Contains(tk.UncommittedWork.Reason, want) {
			t.Errorf("hold reason lacks %q:\n%s", want, tk.UncommittedWork.Reason)
		}
		if !strings.Contains(q.Text, want) {
			t.Errorf("question text lacks %q:\n%s", want, q.Text)
		}
	}
}
