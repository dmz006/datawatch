package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// newFailureEscalationFixture returns a Manager over dir with a fake decompose
// LLM, drained on cleanup. Opening the same dir again simulates a daemon
// restart: the new Manager's store reloads its PRDs from disk.
func newFailureEscalationFixture(t *testing.T, dir string) (*Manager, *API) {
	t.Helper()
	fakeLLM := func(_ DecomposeRequest) (string, error) {
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	// Three retries let a task run four attempts, which the escalation
	// scenarios need; the product default stays one.
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 3
	m, err := NewManager(dir, cfg, fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := NewAPI(m)
	drainOnCleanup(t, m, api)
	return m, api
}

// newFailureEscalationPRD creates an approved PRD with one task whose project
// directory is projectDir. adjust, when non-nil, edits the PRD before approval.
func newFailureEscalationPRD(t *testing.T, m *Manager, projectDir string, adjust func(*PRD)) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("test spec", projectDir, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if adjust != nil {
		adjust(prd)
	}
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd
}

// TestFailureEscalation_RetryPromptCarriesEarlierReports runs a task through
// three attempts. The first attempt preserves its work as a snapshot, the way
// the stall kill does, so its record carries a changed file. The second spawn
// must carry the first attempt's verifier report; the third must carry both
// attempts in order.
func TestFailureEscalation_RetryPromptCarriesEarlierReports(t *testing.T) {
	repo := snapTestRepo(t)
	m, api := newFailureEscalationFixture(t, t.TempDir())

	var mu sync.Mutex
	var reqs []SpawnRequest
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		reqs = append(reqs, req)
		return SpawnResult{SessionID: fmt.Sprintf("sess-%d", len(reqs))}, nil
	}
	verifies := 0
	verify := func(ctx context.Context, prd *PRD, task *Task) (VerificationResult, error) {
		mu.Lock()
		verifies++
		n := verifies
		mu.Unlock()
		switch n {
		case 1:
			// Stand-in for the work the stall kill would preserve: write a file,
			// snapshot it, and record the ref on the task.
			if err := os.WriteFile(filepath.Join(repo, "changed.txt"), []byte("work\n"), 0o644); err != nil {
				return VerificationResult{}, err
			}
			ref, err := SnapshotAttemptWork(ctx, repo, task.ID, 1)
			if err != nil || ref == "" {
				return VerificationResult{}, fmt.Errorf("snapshot: ref=%q err=%v", ref, err)
			}
			if err := m.RecordTaskAttemptSnapshot(prd.ID, task.ID, ref); err != nil {
				return VerificationResult{}, err
			}
			// The stall kill leaves the tree as the attempt found it, so the
			// next attempt does not see this file as foreign drift.
			if err := os.Remove(filepath.Join(repo, "changed.txt")); err != nil {
				return VerificationResult{}, err
			}
			return VerificationResult{OK: false, Summary: "tests still fail in parser",
				Issues: []string{"parser_test.go:12 expects 3 got 2"}}, nil
		case 2:
			return VerificationResult{OK: false, Summary: "second attempt also fails",
				Issues: []string{"parser_test.go:40 panics"}}, nil
		}
		return VerificationResult{OK: true}, nil
	}

	prd := newFailureEscalationPRD(t, m, repo, nil)
	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollPRDTerminal(t, m, prd.ID, 10*time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(reqs) != 3 {
		t.Fatalf("want 3 spawns, got %d", len(reqs))
	}
	if reqs[0].RetryHint != "" {
		t.Fatalf("first spawn has no earlier attempts, RetryHint=%q", reqs[0].RetryHint)
	}

	second := reqs[1].RetryHint
	for _, want := range []string{
		"tests still fail in parser",
		"parser_test.go:12 expects 3 got 2",
		"changed.txt",
		"opencode/",
	} {
		if !strings.Contains(second, want) {
			t.Fatalf("second spawn RetryHint missing %q:\n%s", want, second)
		}
	}

	third := reqs[2].RetryHint
	first := strings.Index(third, "Attempt 1\n")
	later := strings.Index(third, "Attempt 2\n")
	if first < 0 || later < 0 || first > later {
		t.Fatalf("third spawn RetryHint must list attempts 1 and 2 in order:\n%s", third)
	}
	if !strings.Contains(third, "tests still fail in parser") || !strings.Contains(third, "second attempt also fails") {
		t.Fatalf("third spawn RetryHint missing an earlier report:\n%s", third)
	}
}

// TestFailureEscalation_AttemptRecordFields checks the stored record of each
// failed attempt: outcome, category, durations and numbering. The numbering
// must keep counting after a restart that reloads the PRD from the store.
func TestFailureEscalation_AttemptRecordFields(t *testing.T) {
	dir := t.TempDir()
	m, api := newFailureEscalationFixture(t, dir)

	var mu sync.Mutex
	spawns := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		spawns++
		n := spawns
		mu.Unlock()
		if n == 1 {
			// Long enough that the first attempt's duration reads as at least 1s.
			time.Sleep(1100 * time.Millisecond)
		}
		return SpawnResult{SessionID: fmt.Sprintf("sess-%d", n)}, nil
	}
	verifies := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		verifies++
		n := verifies
		mu.Unlock()
		switch n {
		case 1:
			return VerificationResult{}, fmt.Errorf("worker went quiet: %w", ErrWorkerStalled)
		case 2:
			return VerificationResult{OK: false, Category: "content", Summary: "lint failed",
				Issues: []string{"line 4 unused var"}}, nil
		}
		return VerificationResult{OK: true}, nil
	}

	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	taskID := prd.Story[0].Tasks[0].ID
	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollPRDTerminal(t, m, prd.ID, 10*time.Second)

	got, _ := m.Store().GetPRD(prd.ID)
	task := lookupTask(got, taskID)
	if len(task.Attempts) != 2 {
		t.Fatalf("want 2 recorded attempts, got %d: %+v", len(task.Attempts), task.Attempts)
	}
	a1, a2 := task.Attempts[0], task.Attempts[1]

	if a1.Number != 1 || a2.Number != 2 {
		t.Fatalf("numbering: got %d and %d, want 1 and 2", a1.Number, a2.Number)
	}
	if a1.Backend != "opencode" {
		t.Fatalf("attempt 1 backend = %q, want opencode", a1.Backend)
	}
	if a1.Outcome != attemptOutcomeStalled || a1.FailureCategory != "infra" {
		t.Fatalf("attempt 1 outcome/category = %q/%q, want stalled/infra", a1.Outcome, a1.FailureCategory)
	}
	if !strings.Contains(a1.FailureSummary, "worker went quiet") {
		t.Fatalf("attempt 1 FailureSummary = %q, want the stall error", a1.FailureSummary)
	}
	if a1.SnapshotRef != "" || a1.ChangedFiles != nil {
		t.Fatalf("attempt 1 had no snapshot: ref=%q files=%v", a1.SnapshotRef, a1.ChangedFiles)
	}
	if a1.StartedAt.IsZero() || a1.EndedAt.Before(a1.StartedAt) {
		t.Fatalf("attempt 1 times: start=%v end=%v", a1.StartedAt, a1.EndedAt)
	}
	if want := int(a1.EndedAt.Sub(a1.StartedAt) / time.Second); a1.DurationSeconds != want || a1.DurationSeconds < 1 {
		t.Fatalf("attempt 1 DurationSeconds = %d, want %d (at least 1)", a1.DurationSeconds, want)
	}

	if a2.Outcome != attemptOutcomeFailed || a2.FailureCategory != "content" {
		t.Fatalf("attempt 2 outcome/category = %q/%q, want failed/content", a2.Outcome, a2.FailureCategory)
	}
	if a2.VerifierSummary != "lint failed" || !reflect.DeepEqual(a2.VerifierIssues, []string{"line 4 unused var"}) {
		t.Fatalf("attempt 2 verifier report = %q %v", a2.VerifierSummary, a2.VerifierIssues)
	}
	if a2.StartedAt.Before(a1.EndedAt) {
		t.Fatalf("attempt 2 started %v before attempt 1 ended %v", a2.StartedAt, a1.EndedAt)
	}

	// Restart: reopen the same directory and record one more failure. The
	// numbering continues from the reloaded records, and nothing is removed.
	m2, _ := newFailureEscalationFixture(t, dir)
	reloaded, ok := m2.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not reloaded after restart")
	}
	rt := lookupTask(reloaded, taskID)
	if len(rt.Attempts) != 2 || rt.Attempts[0].Outcome != attemptOutcomeStalled || rt.Attempts[1].VerifierSummary != "lint failed" {
		t.Fatalf("records not reloaded intact: %+v", rt.Attempts)
	}
	m2.recordFailedAttempt(context.Background(), reloaded, rt, attemptRun{}, attemptFailure{Outcome: attemptOutcomeFailed, Summary: "after restart"})
	reloaded, _ = m2.Store().GetPRD(prd.ID)
	rt = lookupTask(reloaded, taskID)
	if len(rt.Attempts) != 3 || rt.Attempts[2].Number != 3 {
		t.Fatalf("after restart want attempt 3 appended, got %+v", rt.Attempts)
	}
	if rt.Attempts[2].Backend != "opencode" || rt.Attempts[2].FailureSummary != "after restart" {
		t.Fatalf("attempt recorded after restart = %+v", rt.Attempts[2])
	}
}

// TestFailureEscalation_PromptDisabledKeepsLegacyHint turns escalation off for
// the Automaton. The retry hint must be the legacy verifier text exactly, with
// no escalation report, while the attempt is still recorded.
func TestFailureEscalation_PromptDisabledKeepsLegacyHint(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())

	var mu sync.Mutex
	var reqs []SpawnRequest
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		reqs = append(reqs, req)
		return SpawnResult{SessionID: fmt.Sprintf("sess-%d", len(reqs))}, nil
	}
	verifies := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		defer mu.Unlock()
		verifies++
		if verifies == 1 {
			return VerificationResult{OK: false, Summary: "first failed", Issues: []string{"one"}}, nil
		}
		return VerificationResult{OK: true}, nil
	}

	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{Enabled: escBool(false)}
	})
	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollPRDTerminal(t, m, prd.ID, 10*time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(reqs) != 2 {
		t.Fatalf("want 2 spawns, got %d", len(reqs))
	}
	if want := "first failed\nIssues:\n- one"; reqs[1].RetryHint != want {
		t.Fatalf("legacy RetryHint = %q, want %q", reqs[1].RetryHint, want)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if n := len(lookupTask(got, prd.Story[0].Tasks[0].ID).Attempts); n != 1 {
		t.Fatalf("disabled escalation still records attempts: want 1, got %d", n)
	}
}

// TestFailureEscalation_AttemptChangedFiles checks the changed-file list of an
// attempt snapshot against a real repository, and that every failure to list
// returns nil rather than an error.
func TestFailureEscalation_AttemptChangedFiles(t *testing.T) {
	ctx := context.Background()

	t.Run("lists modified and new files", func(t *testing.T) {
		repo := snapTestRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ref, err := SnapshotAttemptWork(ctx, repo, "task-changed", 1)
		if err != nil || ref == "" {
			t.Fatalf("SnapshotAttemptWork: ref=%q err=%v", ref, err)
		}
		got := attemptChangedFiles(ctx, repo, ref)
		if want := []string{"new.txt", "tracked.txt"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("attemptChangedFiles = %v, want %v", got, want)
		}
	})

	t.Run("caps the list", func(t *testing.T) {
		repo := snapTestRepo(t)
		for i := 0; i < maxAttemptChangedFiles+10; i++ {
			name := filepath.Join(repo, fmt.Sprintf("f%03d.txt", i))
			if err := os.WriteFile(name, []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		ref, err := SnapshotAttemptWork(ctx, repo, "task-cap", 1)
		if err != nil || ref == "" {
			t.Fatalf("SnapshotAttemptWork: ref=%q err=%v", ref, err)
		}
		if got := attemptChangedFiles(ctx, repo, ref); len(got) != maxAttemptChangedFiles {
			t.Fatalf("want %d paths, got %d", maxAttemptChangedFiles, len(got))
		}
	})

	t.Run("nil for empty ref, unknown ref, non-repo and empty dir", func(t *testing.T) {
		repo := snapTestRepo(t)
		if got := attemptChangedFiles(ctx, repo, ""); got != nil {
			t.Fatalf("empty ref: got %v", got)
		}
		if got := attemptChangedFiles(ctx, repo, "refs/datawatch/attempts/none/9"); got != nil {
			t.Fatalf("unknown ref: got %v", got)
		}
		if got := attemptChangedFiles(ctx, t.TempDir(), "refs/datawatch/attempts/none/9"); got != nil {
			t.Fatalf("non-repo dir: got %v", got)
		}
		if got := attemptChangedFiles(ctx, "", "refs/datawatch/attempts/none/9"); got != nil {
			t.Fatalf("empty dir: got %v", got)
		}
	})
}

// TestFailureEscalation_GitEnvDropsMultiplexerVars checks that git calls made
// for an attempt never inherit the terminal multiplexer variables.
func TestFailureEscalation_GitEnvDropsMultiplexerVars(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test,1,0")
	t.Setenv("TMUX_TMPDIR", "/tmp/tmux-test-dir")
	for _, kv := range attemptGitEnv() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			t.Fatalf("attemptGitEnv kept %q", kv)
		}
	}
}

// TestFailureEscalation_OutcomeForError checks the outcome of an adopted
// session's wait: a stall is stalled, any other failure is failed.
func TestFailureEscalation_OutcomeForError(t *testing.T) {
	if got := attemptOutcomeForError(fmt.Errorf("x: %w", ErrWorkerStalled)); got != attemptOutcomeStalled {
		t.Fatalf("stall outcome = %q", got)
	}
	if got := attemptOutcomeForError(errors.New("session died")); got != attemptOutcomeFailed {
		t.Fatalf("other outcome = %q", got)
	}
}
