package autonomous

// Infrastructure retry in the executor loop: an attempt that ends because the
// machinery around the task failed is retried on a back-off without counting
// against the task. The spawn, verify and wait functions are fakes, and
// infraSleep is replaced by a recorder so no test sleeps.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// TestMain keeps the whole package from waiting out real back-off delays: any
// test that meets a transient infrastructure failure (a launch failure, a spawn
// error) retries at once. The tests below replace infraSleep with a recorder.
func TestMain(m *testing.M) {
	infraSleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	os.Exit(m.Run())
}

// infraExecRun is one Automaton with a single task, ready to run.
type infraExecRun struct {
	m      *Manager
	api    *API
	prd    *PRD
	mu     sync.Mutex
	sleeps []time.Duration
	spawns int
}

// newInfraExecRun builds the Automaton. adjust edits the infrastructure retry
// policy (nil keeps the defaults) and prep edits the task before it runs.
func newInfraExecRun(t *testing.T, adjust func(*config.InfraRetryConfig), prep func(*Task)) *infraExecRun {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	r := &infraExecRun{m: m, api: api}

	orig := infraSleep
	infraSleep = func(_ context.Context, d time.Duration) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.sleeps = append(r.sleeps, d)
		return nil
	}
	t.Cleanup(func() { infraSleep = orig })

	if adjust != nil {
		cfg := m.infraRetryConfig()
		adjust(&cfg)
		m.mu.Lock()
		m.cfg.InfraRetry = cfg
		m.mu.Unlock()
	}

	prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Parse the input", Spec: "do the task"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	fresh, _ := m.Store().GetPRD(prd.ID)
	if prep != nil {
		prep(&fresh.Story[0].Tasks[0])
	}
	fresh.Status = PRDApproved
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	r.prd = fresh
	return r
}

func (r *infraExecRun) slept() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.sleeps...)
}

func (r *infraExecRun) spawned() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spawns
}

// passingVerify passes every verification.
func passingVerify(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
	return VerificationResult{OK: true, Summary: "ok"}, nil
}

// verifyFirst returns the first verification outcome once, then passes.
func verifyFirst(first VerificationResult, ferr error) VerifyFn {
	var mu sync.Mutex
	calls := 0
	return func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			return first, ferr
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
}

// spawnN is a test spawn that is told which call number it is.
type spawnN = func(context.Context, SpawnRequest, int) (SpawnResult, error)

// run executes the Automaton with the given fakes and returns the finished
// Automaton and its task. A nil spawn starts every session.
func (r *infraExecRun) run(t *testing.T, spawn spawnN, verify VerifyFn) (*PRD, Task) {
	t.Helper()
	counted := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		r.mu.Lock()
		r.spawns++
		n := r.spawns
		r.mu.Unlock()
		if spawn != nil {
			if _, err := spawn(ctx, req, n); err != nil {
				return SpawnResult{}, err
			}
		}
		return SpawnResult{SessionID: fmt.Sprintf("sess-%d", n)}, nil
	}
	r.api.SetExecutors(counted, verify)
	if err := r.api.Run(r.prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := waitForPRDTerminal(t, r.m, r.prd.ID)
	return got, got.Story[0].Tasks[0]
}

// assertRetriedOnce checks the shared outcome of a successful single retry:
// the task completed, one record of the class with the reason, and nothing
// counted against the task or the Automaton.
func assertRetriedOnce(t *testing.T, r *infraExecRun, got *PRD, task Task, class, reasonPart string) {
	t.Helper()
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %q (error %q), want completed", task.Status, task.Error)
	}
	if len(task.InfraRetries) != 1 {
		t.Fatalf("infra retry records = %d, want 1: %+v", len(task.InfraRetries), task.InfraRetries)
	}
	rec := task.InfraRetries[0]
	if rec.Class != class {
		t.Errorf("record class = %q, want %q", rec.Class, class)
	}
	if !strings.Contains(rec.Reason, reasonPart) {
		t.Errorf("record reason %q lacks %q", rec.Reason, reasonPart)
	}
	if task.EscalationFailures != 0 {
		t.Errorf("EscalationFailures = %d, want 0", task.EscalationFailures)
	}
	if task.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0 (auto_fix_retries untouched)", task.RetryCount)
	}
	if len(got.RetryCountsByCategory) != 0 {
		t.Errorf("RetryCountsByCategory = %v, want none", got.RetryCountsByCategory)
	}
	if got.RetryCeilingTotal != 0 {
		t.Errorf("RetryCeilingTotal = %d, want 0", got.RetryCeilingTotal)
	}
	if r.m.cfg.AutoFixRetries != DefaultConfig().AutoFixRetries {
		t.Errorf("AutoFixRetries changed to %d", r.m.cfg.AutoFixRetries)
	}
	if task.InfraRetryCount != 0 {
		t.Errorf("InfraRetryCount = %d after completion, want 0", task.InfraRetryCount)
	}
	if s := r.slept(); len(s) != 1 || s[0] != 60*time.Second {
		t.Errorf("sleeps = %v, want [1m0s] (the first configured delay)", s)
	}
}

func TestInfraRetry_TransientRetriesAndSucceeds(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify := verifyFirst(VerificationResult{}, fmt.Errorf("write results: %w", syscall.ENOSPC))
	got, task := r.run(t, nil, verify)
	assertRetriedOnce(t, r, got, task, config.InfraRetryClassStorageWrite, "no space left on device")
	if r.spawned() != 2 {
		t.Errorf("spawns = %d, want 2 (a fresh session for the retry)", r.spawned())
	}
	if len(task.Attempts) != 1 || task.Attempts[0].FailureCategory != "infra" {
		t.Errorf("attempts = %+v, want one infra attempt record", task.Attempts)
	}
}

func TestInfraRetry_StorageWriteError(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	// The failure arrives as the text of a failed verification, not an error.
	verify := verifyFirst(VerificationResult{OK: false, Category: "infra", Summary: "could not save: disk quota exceeded"}, nil)
	got, task := r.run(t, nil, verify)
	assertRetriedOnce(t, r, got, task, config.InfraRetryClassStorageWrite, "disk quota exceeded")
}

func TestInfraRetry_SessionKilledBeforeDelivery(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify := verifyFirst(VerificationResult{}, LaunchFailureError("sess-1", "claude-code", "killed"))
	got, task := r.run(t, nil, verify)
	assertRetriedOnce(t, r, got, task, config.InfraRetryClassSessionKilled, "killed")
	if task.StartFailures != 0 {
		t.Errorf("StartFailures = %d, want 0 (not a session start failure)", task.StartFailures)
	}
}

func TestInfraRetry_DaemonRestart(t *testing.T) {
	// The task was left running by an earlier daemon: it holds a session that is
	// gone, and one retry was already used in this incident.
	r := newInfraExecRun(t, nil, func(task *Task) {
		task.Status = TaskInProgress
		task.SessionID = "sess-before-restart"
		task.InfraRetryCount = 1
	})
	got, task := r.run(t, nil, passingVerify)
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %q (error %q), want completed", task.Status, task.Error)
	}
	if len(task.InfraRetries) != 1 {
		t.Fatalf("infra retry records = %d, want 1: %+v", len(task.InfraRetries), task.InfraRetries)
	}
	rec := task.InfraRetries[0]
	if rec.Class != config.InfraRetryClassDaemonRestart || rec.Reason != "daemon restarted while the task was running" {
		t.Errorf("record = %+v", rec)
	}
	if len(task.Attempts) != 0 || task.EscalationFailures != 0 {
		t.Errorf("a restart is not a failed attempt: attempts %d, failures %d", len(task.Attempts), task.EscalationFailures)
	}
	if len(got.RetryCountsByCategory) != 0 || task.RetryCount != 0 {
		t.Errorf("budget consumed: categories %v, retry count %d", got.RetryCountsByCategory, task.RetryCount)
	}
	// The second retry would wait 5 minutes; a restart waits the first delay.
	if s := r.slept(); len(s) != 1 || s[0] != 60*time.Second {
		t.Errorf("sleeps = %v, want [1m0s]", s)
	}
	if r.spawned() != 1 {
		t.Errorf("spawns = %d, want 1", r.spawned())
	}
}

// A restart costs the task no attempt: with one auto-fix retry configured the
// replacement still gets its own attempt and that retry.
func TestInfraRetry_DaemonRestartKeepsAttemptBudget(t *testing.T) {
	r := newInfraExecRun(t, nil, func(task *Task) {
		task.Status = TaskInProgress
		task.SessionID = "sess-before-restart"
	})
	r.m.mu.Lock()
	r.m.cfg.AutoFixRetries = 1
	r.m.mu.Unlock()
	verify := verifyFirst(VerificationResult{OK: false, Summary: "the parser test still fails"}, nil)
	_, task := r.run(t, nil, verify)
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %q (error %q), want completed on the retry", task.Status, task.Error)
	}
	if r.spawned() != 2 {
		t.Errorf("spawns = %d, want 2 (the replacement and its one retry)", r.spawned())
	}
	if len(task.InfraRetries) != 1 || task.InfraRetries[0].Class != config.InfraRetryClassDaemonRestart {
		t.Errorf("infra retries = %+v, want one daemon_restart", task.InfraRetries)
	}
	if len(task.Attempts) != 1 || task.EscalationFailures != 1 {
		t.Errorf("only the verification failure counts: attempts %d, failures %d", len(task.Attempts), task.EscalationFailures)
	}
}

func TestInfraRetry_SpawnError(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	got, task := r.run(t, failFirstSpawnN(errors.New("tmux: server exited unexpectedly")), passingVerify)
	assertRetriedOnce(t, r, got, task, config.InfraRetryClassSpawnError, "tmux: server exited unexpectedly")
	if r.spawned() != 2 {
		t.Errorf("spawns = %d, want 2", r.spawned())
	}
}

// failFirstSpawnN fails the first spawn with err.
func failFirstSpawnN(err error) spawnN {
	return func(_ context.Context, _ SpawnRequest, n int) (SpawnResult, error) {
		if n == 1 {
			return SpawnResult{}, err
		}
		return SpawnResult{}, nil
	}
}

func TestInfraRetry_NonTransientDoesNotRetry(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "the parser test still fails"}, nil
	}
	_, task := r.run(t, nil, verify)
	if len(task.InfraRetries) != 0 || task.InfraRetryCount != 0 {
		t.Errorf("a content failure was retried as infrastructure: %+v", task.InfraRetries)
	}
	if s := r.slept(); len(s) != 0 {
		t.Errorf("slept %v, want no wait", s)
	}
	if task.Status == TaskCompleted {
		t.Errorf("task completed despite a failing verification")
	}
	if task.EscalationFailures == 0 || len(task.Attempts) == 0 {
		t.Errorf("the failure took a different path: failures %d, attempts %d", task.EscalationFailures, len(task.Attempts))
	}
}

// A content failure whose verifier text quotes an infrastructure phrase is
// still a content failure.
func TestInfraRetry_NonTransientQuotingInfraPhraseDoesNotRetry(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{
			OK:      false,
			Summary: "the handler must report 'no space left on device' but returns a generic error",
			Issues:  []string{"expected the message \"worker spawn failed\" in the log"},
		}, nil
	}
	_, task := r.run(t, nil, verify)
	if len(task.InfraRetries) != 0 || task.InfraRetryCount != 0 {
		t.Errorf("a content failure quoting a phrase was retried as infrastructure: %+v", task.InfraRetries)
	}
	if s := r.slept(); len(s) != 0 {
		t.Errorf("slept %v, want no wait", s)
	}
	if task.EscalationFailures == 0 || len(task.Attempts) == 0 {
		t.Errorf("the failure took a different path: failures %d, attempts %d", task.EscalationFailures, len(task.Attempts))
	}
}

func TestInfraRetry_DisabledDoesNotRetry(t *testing.T) {
	off := false
	r := newInfraExecRun(t, func(c *config.InfraRetryConfig) { c.Enabled = &off }, nil)
	_, task := r.run(t, failFirstSpawnN(errors.New("tmux: server exited unexpectedly")), passingVerify)
	if len(task.InfraRetries) != 0 {
		t.Errorf("retried while disabled: %+v", task.InfraRetries)
	}
	if s := r.slept(); len(s) != 0 {
		t.Errorf("slept %v, want no wait", s)
	}
	if task.Status == TaskCompleted || r.spawned() != 1 {
		t.Errorf("status %q after %d spawns, want the spawn error to stand", task.Status, r.spawned())
	}
	// The error text is what it was before the feature existed.
	if !strings.Contains(task.Error, "spawn: tmux: server exited unexpectedly") || strings.Contains(task.Error, InfraSpawnFailureReason) {
		t.Errorf("task error = %q, want the unchanged \"spawn: <cause>\" text", task.Error)
	}
}

// Capacity, workspace-lock and cooldown refusals are waits, never spawn faults.
func TestIsSpawnWaitError(t *testing.T) {
	for _, err := range []error{
		errors.New("start: max sessions reached"),
		errors.New("workspace lock: another agent owns the branch"),
		fmt.Errorf("capacity: %w", capacity.ErrWaitTimeout),
		fmt.Errorf("start: %w", session.ErrGlobalCooldown),
	} {
		if !isSpawnWaitError(err) {
			t.Errorf("isSpawnWaitError(%q) = false, want true", err)
		}
	}
	if isSpawnWaitError(errors.New("tmux: server exited unexpectedly")) {
		t.Errorf("a plain spawn failure was read as a wait")
	}
}

func TestInfraRetry_CategoryNotListedDoesNotRetry(t *testing.T) {
	r := newInfraExecRun(t, func(c *config.InfraRetryConfig) {
		c.Categories = []string{config.InfraRetryClassStorageWrite}
	}, nil)
	_, task := r.run(t, failFirstSpawnN(errors.New("tmux: server exited unexpectedly")), passingVerify)
	if len(task.InfraRetries) != 0 {
		t.Errorf("retried an unlisted class: %+v", task.InfraRetries)
	}
	if s := r.slept(); len(s) != 0 {
		t.Errorf("slept %v, want no wait", s)
	}
	if task.Status == TaskCompleted || r.spawned() != 1 {
		t.Errorf("status %q after %d spawns, want the spawn error to stand", task.Status, r.spawned())
	}
}
