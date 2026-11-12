package main

import (
	"context"
	"os/exec"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// retryWindowEmpty reproduces the verifier's per-attempt evidence on a retry
// whose worker made no new commit: the PreTaskSHA..HEAD window is diffed, and
// the prior-attempt fallback runs with the task's recorded anchors. It returns
// whether the per-attempt evidence is empty and whether the fallback recovers
// the earlier attempt's work, which is the input to the no-output verdict.
func retryWindowEmpty(t *testing.T, dir string, task *autonomouspkg.Task) (perAttemptEmpty, recovered bool, names []string) {
	t.Helper()
	ctx := context.Background()
	committed, err := exec.CommandContext(ctx, "git", "-C", dir, "diff", task.PreTaskSHA+"..HEAD").Output()
	if err != nil {
		t.Fatalf("git diff %s..HEAD: %v", task.PreTaskSHA, err)
	}
	perAttemptEmpty = taskProducedNoOutput(committed, nil, nil, nil)
	hasSHAs := len(task.AttemptCommitSHAs) > 0
	var priorDiff []byte
	var priorOk bool
	if hasSHAs {
		priorDiff, names, priorOk = priorAttemptWorkLanded(ctx, dir, task.BaseSHA, task.AttemptCommitSHAs)
	}
	recovered = resolveEmptyDiffWithPriorWork(perAttemptEmpty, hasSHAs, priorOk)
	if recovered && len(priorDiff) == 0 {
		t.Fatalf("recovered with an empty prior diff")
	}
	return perAttemptEmpty, recovered, names
}

// TestRetryVerdict_CommittedAttemptIsOutputPresent covers the retry after an
// attempt that committed its work. The retry's own PreTaskSHA..HEAD window is
// empty by construction, but the verifier must recover the committed work from
// the prior attempt, so the retry is not failed with a "task produced no
// output" verdict.
func TestRetryVerdict_CommittedAttemptIsOutputPresent(t *testing.T) {
	t.Run("first attempt had a base HEAD", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir)
		commitFile(t, dir, "base.md", "base")
		base := gitHead(t, dir)

		// Attempt 1: captured base HEAD, worker commits feature.md.
		task := &autonomouspkg.Task{BaseSHA: base, PreTaskSHA: base}
		commitFile(t, dir, "feature.md", "attempt 1 work")
		task.RecordAttemptCommit(gitHead(t, dir))

		// Attempt 2: captured HEAD is attempt 1's commit; worker commits nothing.
		task.PreTaskSHA = gitHead(t, dir)

		empty, recovered, names := retryWindowEmpty(t, dir, task)
		if !empty {
			t.Fatalf("per-attempt window not empty; a retry with no new commit must see an empty window")
		}
		if !recovered {
			t.Fatalf("prior attempt's committed work not recovered: retry would be failed as no output")
		}
		if len(names) != 1 || names[0] != "feature.md" {
			t.Fatalf("recovered names = %v, want [feature.md]", names)
		}
	})

	t.Run("first attempt had no HEAD to capture", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir)

		// Attempt 1 starts with no commits: captured PreTaskSHA and BaseSHA are empty.
		task := &autonomouspkg.Task{}
		commitFile(t, dir, "feature.md", "attempt 1 work")
		task.RecordAttemptCommit(gitHead(t, dir))

		// Attempt 2 captures the HEAD that already includes attempt 1's commit.
		task.PreTaskSHA = gitHead(t, dir)

		empty, recovered, names := retryWindowEmpty(t, dir, task)
		if !empty || !recovered {
			t.Fatalf("empty=%v recovered=%v, want both true", empty, recovered)
		}
		if len(names) != 1 || names[0] != "feature.md" {
			t.Fatalf("recovered names = %v, want [feature.md]", names)
		}
	})

	t.Run("no prior commit recorded stays no output", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir)
		commitFile(t, dir, "base.md", "base")
		base := gitHead(t, dir)

		// Attempt 1 committed nothing, so there is no prior work to recover.
		task := &autonomouspkg.Task{BaseSHA: base, PreTaskSHA: base}

		empty, recovered, _ := retryWindowEmpty(t, dir, task)
		if !empty {
			t.Fatalf("per-attempt window not empty")
		}
		if recovered {
			t.Fatalf("recovered = true with no prior attempt commits; a genuinely empty retry must stay no output")
		}
	})
}
