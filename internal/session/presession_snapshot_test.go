// Pre-session snapshot naming: a PRD-owned one-shot Start must name its
// snapshot commit after the PRD and the task title it was given, not after
// an empty PRD id and the generic fallback title.

package session

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// The snapshot subject is built from the PRD id and task title passed in
// StartOptions. The test drives Manager.Start on a dirty temporary repo, with
// the fake tmux and a stub LLM backend, so no real tmux session is started.
// The subject is checked at launch time, when the snapshot commit is HEAD.
func TestPreSessionSnapshotNamesPRDAndTitle(t *testing.T) {
	const prdID = "abcdef0123456789"
	const wantSubject = "snapshot before task Add retry helper (PRD abcdef01)"

	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGitInit(true)
	dir := newDirtyRepo(t)

	launched := false
	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		launched = true
		if got := headSubject(t, projectDir); got != wantSubject {
			t.Errorf("newest commit subject at launch = %q, want %q", got, wantSubject)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	if _, err := mgr.Start(context.Background(), "do the work", "", dir, &StartOptions{
		OneShot:   true,
		PRDID:     prdID,
		TaskTitle: "Add retry helper",
		Git:       true, // git is opt-in
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !launched {
		t.Fatal("LLM backend was not invoked; snapshot assertion did not run")
	}

	// Confirm the snapshot commit itself, not just the launch-time HEAD.
	subject, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if got := strings.TrimSpace(string(subject)); got != wantSubject {
		t.Errorf("newest commit subject = %q, want %q", got, wantSubject)
	}
}
