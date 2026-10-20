// SkipSnapshot (planning/judgment one-shot sessions): Start must not touch
// git at all — no pre-session snapshot commit, nothing staged, nothing
// committed — while a normal PRD one-shot (option unset) keeps snapshotting.

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitLine runs one git command in dir and returns its trimmed stdout.
func gitLine(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// repoState is the git facts the skip-snapshot tests compare before/after.
type repoState struct {
	head     string // git rev-parse HEAD
	staged   string // git diff --cached --name-only
	porcelin string // git status --porcelain
}

func captureRepoState(t *testing.T, dir string) repoState {
	t.Helper()
	return repoState{
		head:     gitLine(t, dir, "rev-parse", "HEAD"),
		staged:   gitLine(t, dir, "diff", "--cached", "--name-only"),
		porcelin: gitLine(t, dir, "status", "--porcelain"),
	}
}

func assertRepoStateUnchanged(t *testing.T, where, dir string, before repoState) {
	t.Helper()
	after := captureRepoState(t, dir)
	if after.head != before.head {
		t.Errorf("%s: HEAD = %q, want unchanged (%q)", where, after.head, before.head)
	}
	if after.staged != before.staged {
		t.Errorf("%s: staged files = %q, want none staged (index must be empty)", where, after.staged)
	}
	if after.porcelin != before.porcelin {
		t.Errorf("%s: status --porcelain changed:\nbefore: %q\nafter:  %q", where, before.porcelin, after.porcelin)
	}
}

// seedBaseCommit creates the repo's first commit from a committed seed file so
// HEAD exists (newDirtyRepo itself leaves an unborn-HEAD repo). The dirty
// untracked work.txt is left alone by the commit.
func seedBaseCommit(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("committed baseline\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", dir, "add", "base.txt").CombinedOutput()
	if err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	out, err = exec.Command("git", "-C", dir, "commit", "-m", "baseline").CombinedOutput()
	if err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
}

// A planning (SkipSnapshot) one-shot Start must leave HEAD, the index and
// the working tree exactly as it found them, at launch and after Start
// returns: no snapshot commit, no staging, no commit of the dirty work.txt.
func TestStart_PlanningSessionLeavesHeadAndIndexUnchanged(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGitInit(true)
	dir := newDirtyRepo(t) // dirty: untracked work.txt
	seedBaseCommit(t, dir) // gives HEAD so "rev-parse HEAD" is defined both sides

	before := captureRepoState(t, dir)
	if !strings.Contains(before.porcelin, "?? work.txt") {
		t.Fatalf("precondition: work.txt should be untracked, status: %q", before.porcelin)
	}

	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		assertRepoStateUnchanged(t, "at launch", projectDir, before)
		// work.txt must still be untracked — never staged or committed.
		if line := gitLine(t, projectDir, "status", "--porcelain"); !strings.Contains(line, "?? work.txt") {
			t.Errorf("work.txt no longer untracked at launch, status: %q", line)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	_, err := mgr.Start(context.Background(), "decompose the PRD", "", dir, &StartOptions{
		OneShot:      true,
		PRDID:        "prd-skip-snapshot",
		SkipSnapshot: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	assertRepoStateUnchanged(t, "after Start", dir, before)
	if line := gitLine(t, dir, "status", "--porcelain"); !strings.Contains(line, "?? work.txt") {
		t.Errorf("work.txt no longer untracked after Start, status: %q", line)
	}
}

// Simulating the decompose write: files the planning session produces inside
// the repo (a verify script under .datawatch/verify/ and its output JSON)
// must survive launch without ever being staged or committed.
func TestStart_PlanningSessionOutputFilesNeverStagedOrCommitted(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGitInit(true)
	dir := newDirtyRepo(t)
	seedBaseCommit(t, dir)

	// The decompose session's own writes, present before Start returns.
	verifyDir := filepath.Join(dir, ".datawatch", "verify")
	if err := os.MkdirAll(verifyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(verifyDir, "x.sh"), []byte("#!/bin/sh\necho verify\n"), 0755); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, ".decompose-output-abc.json")
	if err := os.WriteFile(outFile, []byte(`{"tasks":[]}`), 0644); err != nil {
		t.Fatal(err)
	}

	before := captureRepoState(t, dir)
	if _, err := os.Stat(filepath.Join(verifyDir, "x.sh")); err != nil {
		t.Fatalf("precondition: verify script missing: %v", err)
	}

	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		assertRepoStateUnchanged(t, "at launch", projectDir, before)
		// -uall: list per-file even when the whole .datawatch dir is untracked
		// (plain --porcelain collapses that to "?? .datawatch/").
		status := gitLine(t, projectDir, "status", "--porcelain", "--untracked-files=all")
		for _, want := range []string{
			"?? .datawatch/verify/x.sh",
			"?? .decompose-output-abc.json",
		} {
			if !strings.Contains(status, want) {
				t.Errorf("at launch: want %q present as untracked in status, got: %q", want, status)
			}
		}
		if head := gitLine(t, projectDir, "log", "-1", "--format=%s"); head != "baseline" {
			t.Errorf("at launch: HEAD subject = %q, want still the baseline commit (no snapshot commit)", head)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	_, err := mgr.Start(context.Background(), "decompose the PRD", "", dir, &StartOptions{
		OneShot:      true,
		PRDID:        "prd-skip-snapshot-decomp",
		SkipSnapshot: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	assertRepoStateUnchanged(t, "after Start", dir, before)
	// Never committed: they must not show up in the tree of HEAD.
	if tracked := gitLine(t, dir, "ls-tree", "-r", "HEAD", "--name-only"); strings.Contains(tracked, ".decompose-output-abc.json") || strings.Contains(tracked, "verify/x.sh") {
		t.Errorf("decompose outputs appeared in HEAD's tree (they were committed):\n%s", tracked)
	}
}

// Control: with SkipSnapshot unset (the previous task's default), a PRD
// one-shot Start keeps its existing contract — the dirty tree is still
// snapshot-committed at launch.
func TestStart_TaskSessionStillSnapshotsWhenSkipSnapshotUnset(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGitInit(true)
	dir := newDirtyRepo(t)
	seedBaseCommit(t, dir)
	beforeHead := gitLine(t, dir, "rev-parse", "HEAD")

	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		if head := gitLine(t, projectDir, "rev-parse", "HEAD"); head == beforeHead {
			t.Errorf("task session HEAD unchanged at launch: snapshot commit missing")
		}
		if got := headSubject(t, projectDir); !strings.HasPrefix(got, "pre-session[") {
			t.Errorf("HEAD at launch = %q, want a pre-session[ snapshot commit", got)
		}
		// work.txt was absorbed into the snapshot commit — the normal
		// PRD contract.
		if status := gitLine(t, projectDir, "status", "--porcelain"); strings.Contains(status, "work.txt") {
			t.Errorf("expected work.txt committed in the snapshot, status: %q", status)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	if _, err := mgr.Start(context.Background(), "do the work", "", dir, &StartOptions{
		OneShot: true,
		PRDID:   "prd-snapshot-unset",
		// SkipSnapshot intentionally unset.
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
}
