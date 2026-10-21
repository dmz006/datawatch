package autonomous

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestBlockedPRD_ApproveTaskUnblocksAndRerunsToCompletion — the full
// operator loop for a foreign_file_drift block, end to end through the
// fake spawn/verify harness (no real tmux or sessions):
//
//  1. A real temp git repo where the operator dirtied pre.txt before the
//     task started; the fake "worker" sweeps pre.txt into its own commit.
//     Under the default drift mode (block) the task must end TaskBlocked
//     with a "foreign_file_drift:" error and the PRD must halt at
//     PRDBlocked.
//  2. ApproveTask on the PRDBlocked PRD must be ACCEPTED and must return
//     the task to TaskPending with the stale error cleared (the PRD itself
//     stays PRDBlocked — the run path owns that transition).
//  3. With the drift resolved (the operator's file is now committed, and
//     the retry commits only its own files), the PRD re-runs through the
//     same fake harness to PRDCompleted with the task TaskCompleted.
func TestBlockedPRD_ApproveTaskUnblocksAndRerunsToCompletion(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n")
	driftWrite(t, dir, "pre.txt", "1\n")
	driftGit(t, dir, "add", "base.txt", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	// Operator edit: dirty before the task starts → pre-existing drift
	// the default (block) mode must refuse to commit.
	driftWrite(t, dir, "pre.txt", "operator edit\n")

	prd, err := m.CreatePRD("blocked-drift", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "S", Tasks: []Task{{Title: "T", Spec: "write own.txt", FilesPlanned: []string{"own.txt"}}}},
	}); err != nil {
		t.Fatal(err)
	}

	verifyOK := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	// Attempt 1: the "worker" does its work and sweeps the operator's
	// dirty pre.txt into the same commit.
	spawnSweep := func(ctx context.Context, r SpawnRequest) (SpawnResult, error) {
		sha := driftGit(t, dir, "rev-parse", "HEAD")
		dirty := CapturePreTaskDirtyFiles(ctx, dir, nil)
		driftWrite(t, dir, "own.txt", "task work\n")
		driftGit(t, dir, "add", "own.txt", "pre.txt")
		driftGit(t, dir, "commit", "-q", "-m", "sweep attempt")
		return SpawnResult{SessionID: "s-" + r.TaskID, PreTaskSHA: sha, PreTaskDirtyFiles: dirty}, nil
	}

	got := runToTerminalOrBlocked(t, m, api, prd.ID, spawnSweep, verifyOK)
	if got.Status != PRDBlocked {
		t.Fatalf("PRD status = %s, want PRDBlocked after foreign_file_drift block", got.Status)
	}
	task := got.Story[0].Tasks[0]
	if task.Status != TaskBlocked {
		t.Fatalf("task status = %s, want TaskBlocked", task.Status)
	}
	if !strings.HasPrefix(task.Error, "foreign_file_drift:") || !strings.Contains(task.Error, "pre.txt") {
		t.Fatalf("task error = %q, want \"foreign_file_drift:\" naming pre.txt", task.Error)
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}

	// Operator approves the blocked task: must be ACCEPTED on the
	// PRDBlocked PRD and transition the task back to pending.
	updated, err := m.ApproveTask(prd.ID, task.ID, "operator")
	if err != nil {
		t.Fatalf("ApproveTask on a PRDBlocked PRD must be accepted, got: %v", err)
	}
	appr := updated.Story[0].Tasks[0]
	if appr.Status != TaskPending {
		t.Fatalf("task status after approval = %s, want TaskPending", appr.Status)
	}
	if appr.Error != "" {
		t.Fatalf("task error after approval = %q, want cleared", appr.Error)
	}
	if !appr.Approved || appr.ApprovedBy != "operator" {
		t.Errorf("approval recorded wrong: Approved=%v ApprovedBy=%q", appr.Approved, appr.ApprovedBy)
	}
	if updated.Status != PRDBlocked {
		t.Errorf("PRD status after approval = %s, want PRDBlocked left for the run path", updated.Status)
	}
	approved := false
	for _, d := range updated.Decisions {
		if d.Kind == "approve_task" {
			approved = true
		}
	}
	if !approved {
		t.Error("expected an approve_task Decision to be recorded")
	}

	// Attempt 2: the operator's drift is resolved (pre.txt is now
	// committed, so it's no longer dirty/untracked) and the worker commits
	// only its own file — an in-run edit, which never blocks.
	spawnClean := func(ctx context.Context, r SpawnRequest) (SpawnResult, error) {
		sha := driftGit(t, dir, "rev-parse", "HEAD")
		dirty := CapturePreTaskDirtyFiles(ctx, dir, nil)
		driftWrite(t, dir, "own.txt", "task work v2\n")
		driftGit(t, dir, "add", "own.txt")
		driftGit(t, dir, "commit", "-q", "-m", "task commit")
		return SpawnResult{SessionID: "s-" + r.TaskID + "-retry", PreTaskSHA: sha, PreTaskDirtyFiles: dirty}, nil
	}

	// runToTerminalOrBlocked re-approves + re-runs through the same fake
	// harness (the run-path transition the approval hands off to).
	finished := runToTerminalOrBlocked(t, m, api, prd.ID, spawnClean, verifyOK)
	if finished.Status != PRDCompleted {
		t.Fatalf("PRD status after re-run = %s, want PRDCompleted", finished.Status)
	}
	final := finished.Story[0].Tasks[0]
	if final.Status != TaskCompleted {
		t.Fatalf("task status after re-run = %s, want TaskCompleted", final.Status)
	}
	if final.Error != "" {
		t.Fatalf("task error after re-run = %q, want empty", final.Error)
	}
}
