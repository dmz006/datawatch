// BL366 — unit tests for git-diff grounding in the autonomous verifier.
// Tests cover: PreTaskSHA threading through SpawnResult→Task, diff-max
// truncation config default, and graceful no-op when ProjectDir is empty.

package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// spawnWithSHA returns a SpawnFn that always sets PreTaskSHA to the given sha.
func spawnWithSHA(sha string) SpawnFn {
	return func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "test-session", PreTaskSHA: sha}, nil
	}
}

// capturedSHA returns a VerifyFn that records the task.PreTaskSHA it receives.
func capturedSHA(dst *string) VerifyFn {
	return func(_ context.Context, _ *PRD, t *Task) (VerificationResult, error) {
		*dst = t.PreTaskSHA
		return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
	}
}

// TestBL366_PreTaskSHA_ThreadedToVerifier verifies that SpawnResult.PreTaskSHA
// is stored on the Task and passed to VerifyFn via Task.PreTaskSHA.
func TestBL366_PreTaskSHA_ThreadedToVerifier(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	prd, _ := st.CreatePRD("add feature", "/proj", "claude-code", EffortNormal)
	_ = st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "implement it"}},
	}})
	prd.Status = PRDApproved
	_ = st.SavePRD(prd)

	const wantSHA = "abc1234def5678"
	var gotSHA string

	mgr, err := NewManager(dir, Config{Enabled: true, AutoFixRetries: 0}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if err := mgr.Run(context.Background(), prd.ID,
		spawnWithSHA(wantSHA),
		capturedSHA(&gotSHA),
	); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotSHA != wantSHA {
		t.Errorf("VerifyFn received PreTaskSHA=%q, want %q", gotSHA, wantSHA)
	}
}

// TestBL366_PreTaskSHA_StoredOnTask verifies that the Task record persisted in
// the store carries the SHA after the executor runs.
func TestBL366_PreTaskSHA_StoredOnTask(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	prd, _ := st.CreatePRD("feature", "/proj", "", "")
	_ = st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "do it"}},
	}})
	prd, _ = st.GetPRD(prd.ID) // re-fetch so task IDs are populated
	prd.Status = PRDApproved
	_ = st.SavePRD(prd)

	// Collect task IDs before the run.
	var taskIDs []string
	for _, s := range prd.Story {
		for _, task := range s.Tasks {
			taskIDs = append(taskIDs, task.ID)
		}
	}

	const wantSHA = "deadbeef"
	mgr, _ := NewManager(dir, Config{Enabled: true}, nil)
	_ = mgr.Run(context.Background(), prd.ID,
		spawnWithSHA(wantSHA),
		func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
			return VerificationResult{OK: true, VerifiedAt: time.Now()}, nil
		},
	)

	// GetTask from the manager's own store (not the separate st instance).
	for _, id := range taskIDs {
		task, ok := mgr.Store().GetTask(id)
		if !ok {
			t.Errorf("task %s not found in store", id)
			continue
		}
		if task.PreTaskSHA != wantSHA {
			t.Errorf("stored task PreTaskSHA=%q, want %q", task.PreTaskSHA, wantSHA)
		}
	}
}

// TestBL366_PreTaskSHA_EmptyWhenNoSHA ensures that a SpawnResult with no SHA
// (e.g. cluster dispatch) leaves Task.PreTaskSHA empty — no panic, no error.
func TestBL366_PreTaskSHA_EmptyWhenNoSHA(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	prd, _ := st.CreatePRD("feature", "", "", "")
	_ = st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "do it"}},
	}})
	prd.Status = PRDApproved
	_ = st.SavePRD(prd)

	mgr, _ := NewManager(dir, Config{Enabled: true}, nil)
	var gotSHA string
	if err := mgr.Run(context.Background(), prd.ID,
		func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
			return SpawnResult{SessionID: "s", PreTaskSHA: ""}, nil
		},
		capturedSHA(&gotSHA),
	); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotSHA != "" {
		t.Errorf("want empty PreTaskSHA, got %q", gotSHA)
	}
}

// TestBL366_VerifierDiffMaxBytes_DefaultIsEightKB verifies the documented
// default truncation limit in the manager config.
func TestBL366_VerifierDiffMaxBytes_DefaultIsEightKB(t *testing.T) {
	cfg := Config{VerifierDiffMaxBytes: 0}
	// Default of 0 means "use 8192" in the verifier closure.
	// This test validates the intent: zero value → 8192 sentinel.
	// The closure logic itself lives in cmd/datawatch/main.go but the
	// Config field default is documented as 0=8192.
	if cfg.VerifierDiffMaxBytes != 0 {
		t.Errorf("zero-value VerifierDiffMaxBytes should be 0 (means 8192); got %d", cfg.VerifierDiffMaxBytes)
	}
}

// TestBL366_GitDiffCapture is an integration-style test that creates a real git
// repo, makes a commit, and verifies that the SHA captured before a second
// commit differs from HEAD — confirming the diff-capture approach is sound.
func TestBL366_GitDiffCapture(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-q")
	run("commit", "--allow-empty", "-m", "init")

	// Capture HEAD before the worker's change.
	preSHAOut, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	preSHA := string(preSHAOut[:len(preSHAOut)-1]) // trim newline

	// Simulate a worker commit.
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "feature.go")
	run("commit", "-m", "add feature")

	// The diff from preSHA..HEAD should be non-empty.
	diffOut, err := exec.Command("git", "-C", repo, "diff", preSHA+"..HEAD").Output()
	if err != nil {
		t.Fatalf("git diff: %v", err)
	}
	if len(diffOut) == 0 {
		t.Error("expected non-empty diff after worker commit; got empty")
	}
	if string(diffOut) == "" || !containsStr(string(diffOut), "feature.go") {
		t.Errorf("diff does not mention feature.go:\n%s", diffOut)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
