package autonomous

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// gitHeadIn returns the HEAD SHA of dir, or "" when dir has no commit or is not
// a repo. It mirrors the production pre-task capture in cmd/datawatch/main.go.
func gitHeadIn(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// initRetryRepo makes dir a git repo with one base commit and returns its HEAD.
func initRetryRepo(t *testing.T, dir string) string {
	t.Helper()
	driftGit(t, dir, "init", "-q")
	driftGit(t, dir, "config", "user.email", "t@t.test")
	driftGit(t, dir, "config", "user.name", "t")
	return commitRetryWork(t, dir, "base.txt", "base")
}

// commitRetryWork writes name, commits it and returns the new HEAD.
func commitRetryWork(t *testing.T, dir, name, body string) string {
	t.Helper()
	driftWrite(t, dir, name, body)
	driftGit(t, dir, "add", "-A")
	driftGit(t, dir, "commit", "-q", "-m", name)
	return driftGit(t, dir, "rev-parse", "HEAD")
}

// capturingSpawn mirrors the production spawn: it captures HEAD in the task's
// project dir BEFORE the worker runs, then (when commit is set) the worker
// commits its work. pre records each attempt's captured pre-worker HEAD.
func capturingSpawn(t *testing.T, pre *[]string, commit bool) SpawnFn {
	return func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		sha := gitHeadIn(req.ProjectDir)
		*pre = append(*pre, sha)
		n := len(*pre)
		if commit {
			commitRetryWork(t, req.ProjectDir, fmt.Sprintf("attempt-%d.txt", n), "work")
		}
		return SpawnResult{SessionID: fmt.Sprintf("sess-%d", n), PreTaskSHA: sha}, nil
	}
}

// runRetryTask runs one task seeded from seed through the real executor, with
// the first failures verifications failing so the task auto-retries. It returns
// the final task and the PreTaskSHA the verifier saw on each attempt.
func runRetryTask(t *testing.T, projectDir string, seed Task, spawn SpawnFn, failures int) (Task, []string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), Config{Enabled: true, AutoFixRetries: failures}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", projectDir, "", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{seed}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	var seen []string
	verifyCalls := 0
	if err := m.Run(context.Background(), prd.ID, spawn,
		func(_ context.Context, _ *PRD, tk *Task) (VerificationResult, error) {
			verifyCalls++
			seen = append(seen, tk.PreTaskSHA)
			if verifyCalls <= failures {
				return VerificationResult{OK: false, Summary: "not yet done", VerifiedAt: time.Now()}, nil
			}
			return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
		},
	); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	return got.Story[0].Tasks[0], seen
}

// TestRetryPreTaskSHA_EachAttemptCapturesItsOwnPreWorkerHead runs a real temp
// git repo through attempt 1 (commits work) and an auto-retry. Each attempt's
// PreTaskSHA must be the HEAD captured before that attempt's worker ran, and
// BaseSHA must keep the first attempt's HEAD.
func TestRetryPreTaskSHA_EachAttemptCapturesItsOwnPreWorkerHead(t *testing.T) {
	repo := t.TempDir()
	h0 := initRetryRepo(t, repo)

	var pre []string
	task, seen := runRetryTask(t, repo, Task{Title: "T1", Spec: "do it"}, capturingSpawn(t, &pre, true), 1)

	if len(pre) != 2 {
		t.Fatalf("spawn calls = %d, want 2", len(pre))
	}
	// Attempt 1 starts on the base commit. Attempt 2 starts after attempt 1's
	// commit, which is HEAD~1 once attempt 2 has committed on top of it.
	attempt1Commit := driftGit(t, repo, "rev-parse", "HEAD~1")
	if pre[0] != h0 {
		t.Errorf("attempt 1 PreTaskSHA = %q, want base %q", pre[0], h0)
	}
	if pre[1] != attempt1Commit {
		t.Errorf("attempt 2 PreTaskSHA = %q, want attempt-1 commit %q", pre[1], attempt1Commit)
	}
	if len(seen) != 2 || seen[0] != pre[0] || seen[1] != pre[1] {
		t.Errorf("verifier saw PreTaskSHA %v, want per-attempt %v", seen, pre)
	}
	if task.BaseSHA != h0 {
		t.Errorf("BaseSHA = %q, want first-attempt base %q (retries must never overwrite it)", task.BaseSHA, h0)
	}
	if task.PreTaskSHA != pre[1] {
		t.Errorf("final PreTaskSHA = %q, want latest attempt %q", task.PreTaskSHA, pre[1])
	}
}

// TestRetryPreTaskSHA_SingleAttemptPathUnchanged covers the non-retry path: one
// attempt that passes verification sets BaseSHA and PreTaskSHA to the same
// pre-worker HEAD, and the spawn runs exactly once.
func TestRetryPreTaskSHA_SingleAttemptPathUnchanged(t *testing.T) {
	repo := t.TempDir()
	h0 := initRetryRepo(t, repo)

	var pre []string
	task, seen := runRetryTask(t, repo, Task{Title: "T1", Spec: "do it"}, capturingSpawn(t, &pre, true), 0)

	if len(pre) != 1 || pre[0] != h0 {
		t.Fatalf("spawn PreTaskSHAs = %v, want [%s]", pre, h0)
	}
	if len(seen) != 1 || seen[0] != h0 {
		t.Errorf("verifier saw %v, want [%s]", seen, h0)
	}
	if task.BaseSHA != h0 || task.PreTaskSHA != h0 {
		t.Errorf("BaseSHA=%q PreTaskSHA=%q, want both %q", task.BaseSHA, task.PreTaskSHA, h0)
	}
}

// TestRetryPreTaskSHA_EdgeCases covers the capture outcomes that must never be
// treated as an anchor: a repo with no commits (empty SHA on attempt 1), a repo
// that never commits (empty SHA on every attempt), and a non-git project dir.
func TestRetryPreTaskSHA_EdgeCases(t *testing.T) {
	t.Run("empty first capture then committed retry", func(t *testing.T) {
		repo := t.TempDir()
		driftGit(t, repo, "init", "-q")
		driftGit(t, repo, "config", "user.email", "t@t.test")
		driftGit(t, repo, "config", "user.name", "t")

		var pre []string
		task, _ := runRetryTask(t, repo, Task{Title: "T1", Spec: "do it"}, capturingSpawn(t, &pre, true), 1)

		if len(pre) != 2 || pre[0] != "" {
			t.Fatalf("spawn PreTaskSHAs = %q, want attempt 1 empty", pre)
		}
		if task.BaseSHA != "" {
			t.Errorf("BaseSHA = %q, want empty (attempt 1 had no HEAD to anchor)", task.BaseSHA)
		}
		if task.PreTaskSHA != pre[1] || pre[1] == "" {
			t.Errorf("PreTaskSHA = %q, want attempt 2's captured HEAD %q", task.PreTaskSHA, pre[1])
		}
	})

	t.Run("no commits on any attempt", func(t *testing.T) {
		repo := t.TempDir()
		driftGit(t, repo, "init", "-q")

		var pre []string
		task, seen := runRetryTask(t, repo, Task{Title: "T1", Spec: "do it"}, capturingSpawn(t, &pre, false), 1)

		if len(pre) != 2 || pre[0] != "" || pre[1] != "" {
			t.Fatalf("spawn PreTaskSHAs = %q, want two empty captures", pre)
		}
		if len(seen) != 2 || seen[0] != "" || seen[1] != "" {
			t.Errorf("verifier saw %q, want two empty PreTaskSHAs", seen)
		}
		if task.BaseSHA != "" || task.PreTaskSHA != "" {
			t.Errorf("BaseSHA=%q PreTaskSHA=%q, want both empty", task.BaseSHA, task.PreTaskSHA)
		}
	})

	t.Run("non-git project dir", func(t *testing.T) {
		dir := t.TempDir()

		var pre []string
		task, seen := runRetryTask(t, dir, Task{Title: "T1", Spec: "do it"}, capturingSpawn(t, &pre, false), 1)

		if len(pre) != 2 || pre[0] != "" || pre[1] != "" {
			t.Fatalf("spawn PreTaskSHAs = %q, want two empty captures for a non-git dir", pre)
		}
		if len(seen) != 2 {
			t.Fatalf("verifier calls = %d, want 2", len(seen))
		}
		if task.BaseSHA != "" || task.PreTaskSHA != "" {
			t.Errorf("BaseSHA=%q PreTaskSHA=%q, want both empty for a non-git dir", task.BaseSHA, task.PreTaskSHA)
		}
	})
}

// TestRetryPreTaskSHA_ResumedLiveSessionKeepsAnchors covers the resumed-session
// path: a task still verifying a live session after a daemon restart must not
// spawn again, and its recorded PreTaskSHA and BaseSHA must survive untouched.
func TestRetryPreTaskSHA_ResumedLiveSessionKeepsAnchors(t *testing.T) {
	seed := Task{
		Title:      "T1",
		Spec:       "do it",
		Status:     TaskVerifying,
		SessionID:  "sess-live",
		PreTaskSHA: "pre-live",
		BaseSHA:    "base-live",
	}
	spawnCalls := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCalls++
		return SpawnResult{SessionID: "sess-new", PreTaskSHA: "pre-new"}, nil
	}

	m, err := NewManager(t.TempDir(), Config{Enabled: true}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetSessionAliveFn(func(id string) bool { return id == "sess-live" })
	prd, err := m.CreatePRD("spec", "/proj", "", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{seed}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	if err := m.Run(context.Background(), prd.ID, spawn,
		func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
			return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
		},
	); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if spawnCalls != 0 {
		t.Fatalf("spawn calls = %d, want 0 (live session must be resumed, not respawned)", spawnCalls)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.PreTaskSHA != "pre-live" {
		t.Errorf("PreTaskSHA = %q, want untouched %q", tk.PreTaskSHA, "pre-live")
	}
	if tk.BaseSHA != "base-live" {
		t.Errorf("BaseSHA = %q, want untouched %q", tk.BaseSHA, "base-live")
	}
}

// TestRetryPreTaskSHA_ReanchorsAfterHumanReset covers a human reset: the reset
// clears BaseSHA on purpose so the fresh run re-captures it, even though the
// task already has prior attempts on record. The attempt-0 gate must allow that.
func TestRetryPreTaskSHA_ReanchorsAfterHumanReset(t *testing.T) {
	const fresh = "sha-after-reset"
	seed := Task{Title: "T1", Spec: "do it", PriorAttemptCount: 1}
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-reset", PreTaskSHA: fresh}, nil
	}

	tk, _ := runRetryTask(t, "/proj", seed, spawn, 0)
	if tk.BaseSHA != fresh {
		t.Errorf("BaseSHA = %q, want re-captured %q after a human reset", tk.BaseSHA, fresh)
	}
	if tk.PreTaskSHA != fresh {
		t.Errorf("PreTaskSHA = %q, want %q", tk.PreTaskSHA, fresh)
	}
}
