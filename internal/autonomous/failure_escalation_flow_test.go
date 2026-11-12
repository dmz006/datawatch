package autonomous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The end-to-end flow of failure escalation, run entirely on fakes: a fake
// spawn that records every request and advances a fake clock by each
// attempt's duration, a fake verifier that fails the parser task a set number
// of times and snapshots the work of each failed attempt, and a fake alert
// sink. The global policy is a two-step ladder: a local model for one attempt,
// then a stronger Claude model for every attempt after it.

const (
	flowParserTask    = "Parser task"
	flowDependentTask = "Dependent task"

	flowLocalModel  = "ollama/qwen3.8:27b"
	flowStrongModel = "claude-opus-5-5"
	flowRetryModel  = "claude-sonnet-5-5"

	flowReviewAlert = "Automaton task needs review"
)

// flowAttemptSeconds is the duration of each spawn, in spawn order. The last
// entry repeats for any spawn past the end of the list.
var flowAttemptSeconds = []int{60, 90, 120, 30, 45}

// flowChangedFiles is the file each failing parser attempt writes, in order.
var flowChangedFiles = []string{"parser.go", "lexer.go", "ast.go", "tokens.go"}

// fakeAttemptClock is a manually advanced time source for attempt durations.
type fakeAttemptClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeAttemptClock() *fakeAttemptClock {
	return &fakeAttemptClock{now: time.Now()}
}

func (c *fakeAttemptClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeAttemptClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// flowRig is one Automaton run against the fakes.
type flowRig struct {
	m      *Manager
	api    *API
	clk    *fakeAttemptClock
	alerts holdAlertLog
	spawns escalationSpawnLog
	repo   string
}

// flowGlobalPolicy is the global default the flow runs under: a local model
// for the first attempt, then a stronger Claude model for every attempt after.
func flowGlobalPolicy() FailureEscalationPolicy {
	return FailureEscalationPolicy{
		Enabled:     escalationOn(true),
		MaxFailures: 3,
		Steps: []FailureEscalationStep{
			{Backend: escPiBackend, Model: flowLocalModel, Attempts: 1},
			{Backend: escClaudeBackend, Model: flowStrongModel},
		},
	}
}

// newFlowRig builds a Manager under the global policy with the fake clock and
// alert sink wired, and a git repository for the parser task's work.
func newFlowRig(t *testing.T, global FailureEscalationPolicy, failures int) *flowRig {
	t.Helper()
	r := &flowRig{clk: newFakeAttemptClock(), repo: snapTestRepo(t)}
	r.m, r.api = newReviewFixture(t, global, nil)
	r.m.attemptClock = r.clk.Now
	r.m.SetAlertSink(r.alerts.sink)
	r.api.SetExecutors(r.spawn, flowParserVerifier(t, r.m, r.repo, failures))
	return r
}

// spawn advances the fake clock by the duration of this spawn, then records
// the request.
func (r *flowRig) spawn(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
	n := len(r.spawns.workers())
	secs := flowAttemptSeconds[min(n, len(flowAttemptSeconds)-1)]
	r.clk.advance(time.Duration(secs) * time.Second)
	return r.spawns.spawn(ctx, req)
}

// run starts prd and waits until it is terminal or held for review.
func (r *flowRig) run(t *testing.T, prd *PRD) {
	t.Helper()
	if err := r.api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollEscalationSettled(t, r.m, prd.ID, 10*time.Second)
	waitExecutorGone(t, r.api, prd.ID, 10*time.Second)
}

// flowParserVerifier fails the first failures verifications of the parser
// task and passes the rest. Each failing attempt writes its own file,
// snapshots it the way a stall kill keeps work, and removes it so the next
// attempt starts from a clean tree. Its report names the file and the failing
// test. Every other task passes.
func flowParserVerifier(t *testing.T, m *Manager, repo string, failures int) VerifyFn {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	return func(ctx context.Context, prd *PRD, task *Task) (VerificationResult, error) {
		if task.Title != flowParserTask {
			return VerificationResult{OK: true, Summary: "tests pass"}, nil
		}
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n > failures {
			return VerificationResult{OK: true, Summary: "parser tests pass"}, nil
		}
		path := filepath.Join(repo, flowChangedFiles[min(n, len(flowChangedFiles))-1])
		if err := os.WriteFile(path, []byte("work\n"), 0o644); err != nil {
			return VerificationResult{}, err
		}
		ref, err := SnapshotAttemptWork(ctx, repo, task.ID, n)
		if err != nil || ref == "" {
			return VerificationResult{}, fmt.Errorf("snapshot attempt %d: ref=%q err=%v", n, ref, err)
		}
		if err := m.RecordTaskAttemptSnapshot(prd.ID, task.ID, ref); err != nil {
			return VerificationResult{}, err
		}
		if err := os.Remove(path); err != nil {
			return VerificationResult{}, err
		}
		return VerificationResult{
			OK:      false,
			Summary: fmt.Sprintf("parser tests fail on attempt %d", n),
			Issues:  []string{fmt.Sprintf("parser_test.go:%d expects 3 tokens, got 2", 10*n)},
		}, nil
	}
}

// addFlowPRD creates the two-task Automaton: the parser task, and a dependent
// task that waits on it. parserTask, when non-nil, adjusts the parser task.
func (r *flowRig) addFlowPRD(t *testing.T, parserTask func(*Task)) *PRD {
	t.Helper()
	parser := Task{Title: flowParserTask, Spec: "make the parser pass", ProjectDir: r.repo}
	if parserTask != nil {
		parserTask(&parser)
	}
	return newHoldPRD(t, r.m, []Task{
		parser,
		{Title: flowDependentTask, Spec: "build on the parser", DependsOn: []string{flowParserTask}, ProjectDir: r.repo},
	}, nil)
}

// flowWorker is the backend/model pair the spawn log records for a worker.
func flowWorker(backend, model string) string {
	return backend + "/" + model
}

// flowHeldRow returns the review row of the given Automaton and task.
func flowHeldRow(rows []HeldTaskStatus, automatonID, taskID string) (HeldTaskStatus, bool) {
	for _, r := range rows {
		if r.AutomatonID == automatonID && r.TaskID == taskID {
			return r, true
		}
	}
	return HeldTaskStatus{}, false
}

// flowCountAlerts counts the alerts raised with the review title.
func flowCountAlerts(l *holdAlertLog) int {
	n := 0
	for _, a := range l.all() {
		if a.title == flowReviewAlert {
			n++
		}
	}
	return n
}

// TestFailureEscalation_FullFlow drives one Automaton from its first failed
// attempt through the hold, the operator's retry and completion. The subtests
// cover the cancel path and a task-level MaxFailures that overrides the global
// default.
func TestFailureEscalation_FullFlow(t *testing.T) {
	t.Run("hold_then_operator_retry_completes", func(t *testing.T) {
		r := newFlowRig(t, flowGlobalPolicy(), 3)
		prd := r.addFlowPRD(t, nil)
		parserID := taskByTitle(t, r.m, prd.ID, flowParserTask).ID
		depID := taskByTitle(t, r.m, prd.ID, flowDependentTask).ID
		localWorker := flowWorker(escPiBackend, flowLocalModel)
		strongWorker := flowWorker(escClaudeBackend, flowStrongModel)

		r.run(t, prd)

		// The hold: three attempts, the local worker first, then the stronger
		// Claude model for the second and third.
		workers := r.spawns.workers()
		if len(workers) != 3 {
			t.Fatalf("spawns before the hold = %v, want exactly 3 (the parser task only)", workers)
		}
		if workers[0] != localWorker || workers[1] != strongWorker || workers[2] != strongWorker {
			t.Fatalf("attempt workers = %v, want [%s %s %s]", workers, localWorker, strongWorker, strongWorker)
		}
		reqs := spawnRequests(&r.spawns)
		if reqs[0].RetryHint != "" {
			t.Fatalf("first attempt has no earlier attempts, RetryHint=%q", reqs[0].RetryHint)
		}
		for _, want := range []string{"parser tests fail on attempt 1", "parser_test.go:10", "parser.go"} {
			if !strings.Contains(reqs[1].RetryHint, want) {
				t.Fatalf("attempt 2 RetryHint missing %q:\n%s", want, reqs[1].RetryHint)
			}
		}
		third := reqs[2].RetryHint
		first, second := strings.Index(third, "Attempt 1\n"), strings.Index(third, "Attempt 2\n")
		if first < 0 || second < 0 || first > second {
			t.Fatalf("attempt 3 RetryHint must list attempts 1 and 2 in order:\n%s", third)
		}
		for _, want := range []string{"parser tests fail on attempt 1", "parser tests fail on attempt 2", "parser.go", "lexer.go"} {
			if !strings.Contains(third, want) {
				t.Fatalf("attempt 3 RetryHint missing %q:\n%s", want, third)
			}
		}

		parser := taskByTitle(t, r.m, prd.ID, flowParserTask)
		if parser.Status != TaskReviewNeeded {
			t.Fatalf("parser status = %q, want %q", parser.Status, TaskReviewNeeded)
		}
		if parser.FailureReason == nil || parser.FailureReason.Source != failureEscalationHoldSource || parser.FailureReason.Retryable {
			t.Fatalf("hold FailureReason = %+v, want a non-retryable hold", parser.FailureReason)
		}
		if len(parser.Attempts) != 3 {
			t.Fatalf("recorded attempts = %d, want 3", len(parser.Attempts))
		}
		for i, a := range parser.Attempts {
			if a.Number != i+1 || a.Outcome != attemptOutcomeFailed || !a.Escalated {
				t.Fatalf("attempt %d record = number %d outcome %q escalated %v", i+1, a.Number, a.Outcome, a.Escalated)
			}
		}
		if got := []int{parser.Attempts[0].DurationSeconds, parser.Attempts[1].DurationSeconds, parser.Attempts[2].DurationSeconds}; got[0] != 60 || got[1] != 90 || got[2] != 120 {
			t.Fatalf("attempt durations = %v, want [60 90 120] from the fake clock", got)
		}
		if parser.Attempts[0].Backend != escPiBackend || parser.Attempts[0].Model != flowLocalModel {
			t.Fatalf("attempt 1 worker = %s/%s, want the local step", parser.Attempts[0].Backend, parser.Attempts[0].Model)
		}

		sum := parser.ReviewSummary
		if sum == nil {
			t.Fatal("held task has no ReviewSummary")
		}
		if sum.FailureCount != 3 || sum.TotalSeconds != 270 {
			t.Fatalf("ReviewSummary count %d total %d s, want 3 and 270 s", sum.FailureCount, sum.TotalSeconds)
		}
		if !sum.HeldAt.Equal(r.clk.Now()) {
			t.Fatalf("HeldAt = %v, want the fake clock reading %v at the hold", sum.HeldAt, r.clk.Now())
		}
		wantReport := []string{
			"Attempt 1\n", "Attempt 2\n", "Attempt 3\n",
			"Worker: " + localWorker, "Worker: " + strongWorker,
			"Duration: 60 s", "Duration: 90 s", "Duration: 120 s",
			"- parser.go", "- lexer.go", "- ast.go",
			"parser tests fail on attempt 1", "parser tests fail on attempt 2", "parser tests fail on attempt 3",
			"Total time spent: 270 s across 3 recorded attempts.",
		}
		for _, want := range wantReport {
			if !strings.Contains(sum.Text, want) {
				t.Fatalf("review report missing %q:\n%s", want, sum.Text)
			}
		}

		if n := flowCountAlerts(&r.alerts); n != 1 {
			t.Fatalf("review alerts = %d, want exactly 1", n)
		}
		dep := taskByTitle(t, r.m, prd.ID, flowDependentTask)
		if normTaskStatus(dep.Status) != TaskPending || r.spawns.spawnsFor(depID) != 0 {
			t.Fatalf("dependent status %q with %d spawns, want pending and never started", dep.Status, r.spawns.spawnsFor(depID))
		}

		blocked, _ := r.m.Store().GetPRD(prd.ID)
		// The reason names the held task by ID, the way the blocked view shows it.
		if blocked.Status != PRDBlocked || blocked.BlockedReason != "task "+parserID+" needs operator review" {
			t.Fatalf("Automaton status %q reason %q, want blocked on the review", blocked.Status, blocked.BlockedReason)
		}

		view := BuildSummaryView(blocked)
		if len(view.ReviewNeeded) != 1 || view.ReviewNeeded[0].TaskID != parserID || view.ReviewNeeded[0].FailureCount != 3 {
			t.Fatalf("SummaryView.review_needed = %+v, want the held parser task after 3 failures", view.ReviewNeeded)
		}
		if _, ok := flowHeldRow(r.m.Status().ReviewNeeded, prd.ID, parserID); !ok {
			t.Fatalf("status output ReviewNeeded = %+v, want the held parser task", r.m.Status().ReviewNeeded)
		}

		// The operator changes the worker to a Claude Sonnet model and narrows
		// the scope to the parser file. The fourth attempt must run on the new
		// worker, pass, and let the dependent run.
		change := ReviewResolution{
			Action:  reviewActionRetry,
			Backend: escClaudeBackend,
			Model:   flowRetryModel,
			Effort:  EffortNormal,
			Files:   []string{"parser.go"},
			Note:    "try a stronger model on the parser file only",
		}
		if _, err := r.api.ResolveTaskReview(prd.ID, parserID, change, "operator-test"); err != nil {
			t.Fatalf("ResolveTaskReview retry: %v", err)
		}
		pollEscalationSettled(t, r.m, prd.ID, 10*time.Second)

		workers = r.spawns.workers()
		if len(workers) != 5 {
			t.Fatalf("spawns after the operator retry = %v, want 5 (4 parser attempts and the dependent)", workers)
		}
		if got, want := workers[3], flowWorker(escClaudeBackend, flowRetryModel); got != want {
			t.Fatalf("fourth attempt worker = %s, want the operator's %s", got, want)
		}
		reqs = spawnRequests(&r.spawns)
		if !strings.Contains(reqs[3].RetryHint, "narrowed the files this task may change to: parser.go") {
			t.Fatalf("fourth attempt RetryHint does not carry the narrowed scope:\n%s", reqs[3].RetryHint)
		}
		if !strings.Contains(reqs[3].RetryHint, "Attempt 3\n") {
			t.Fatalf("fourth attempt RetryHint does not carry the held report:\n%s", reqs[3].RetryHint)
		}

		parser = taskByTitle(t, r.m, prd.ID, flowParserTask)
		if parser.Status != TaskCompleted {
			t.Fatalf("parser status after the retry = %q, want %q", parser.Status, TaskCompleted)
		}
		if parser.ReviewSummary != nil || len(parser.FilesPlanned) != 1 || parser.FilesPlanned[0] != "parser.go" {
			t.Fatalf("after retry ReviewSummary %v, FilesPlanned %v; want cleared and [parser.go]", parser.ReviewSummary, parser.FilesPlanned)
		}
		if !parser.OperatorWorkerSet || parser.Backend != escClaudeBackend || parser.Model != flowRetryModel {
			t.Fatalf("parser worker %s/%s operator-set %v, want the operator's worker", parser.Backend, parser.Model, parser.OperatorWorkerSet)
		}
		if got := taskByTitle(t, r.m, prd.ID, flowDependentTask); got.Status != TaskCompleted {
			t.Fatalf("dependent status = %q, want %q after the parser passed", got.Status, TaskCompleted)
		}
		done, _ := r.m.Store().GetPRD(prd.ID)
		if done.Status != PRDCompleted {
			t.Fatalf("Automaton status = %q, want %q", done.Status, PRDCompleted)
		}
		if !prdHasDecisionKind(r.m, prd.ID, reviewDecisionKind) {
			t.Fatal("the operator's retry left no review_resolved decision")
		}
		if n := flowCountAlerts(&r.alerts); n != 1 {
			t.Fatalf("review alerts after the flow = %d, want still exactly 1", n)
		}
	})

	t.Run("cancel_after_review", func(t *testing.T) {
		r := newFlowRig(t, flowGlobalPolicy(), 3)
		prd := r.addFlowPRD(t, nil)
		parserID := taskByTitle(t, r.m, prd.ID, flowParserTask).ID

		r.run(t, prd)
		if s := taskByTitle(t, r.m, prd.ID, flowParserTask); s.Status != TaskReviewNeeded {
			t.Fatalf("parser status = %q, want %q before the cancel", s.Status, TaskReviewNeeded)
		}

		cancel := ReviewResolution{Action: reviewActionCancel, Note: "the parser is not worth another try"}
		if _, err := r.api.ResolveTaskReview(prd.ID, parserID, cancel, "operator-test"); err != nil {
			t.Fatalf("ResolveTaskReview cancel: %v", err)
		}
		pollEscalationSettled(t, r.m, prd.ID, 10*time.Second)

		if s := taskByTitle(t, r.m, prd.ID, flowParserTask); s.Status != TaskCancelled {
			t.Fatalf("parser status after cancel = %q, want %q", s.Status, TaskCancelled)
		}
		if n := len(r.spawns.workers()); n != 3 {
			t.Fatalf("spawns after cancel = %d, want 3: a cancelled task is not retried", n)
		}
		if n := flowCountAlerts(&r.alerts); n != 1 {
			t.Fatalf("review alerts = %d, want exactly 1", n)
		}
		if !prdHasDecisionKind(r.m, prd.ID, reviewDecisionKind) {
			t.Fatal("the operator's cancel left no review_resolved decision")
		}
		// A cancelled dependency is not fulfilled, so its dependent fails with
		// the cancel named as the reason and never starts.
		dep := taskByTitle(t, r.m, prd.ID, flowDependentTask)
		if dep.Status != TaskFailed || !strings.Contains(dep.Error, "was cancelled") {
			t.Fatalf("dependent status %q error %q, want failed because its dependency was cancelled", dep.Status, dep.Error)
		}
		if n := r.spawns.spawnsFor(dep.ID); n != 0 {
			t.Fatalf("dependent spawns = %d, want 0", n)
		}
		// The failed dependent takes the Automaton down with it, as it would
		// for any failed task; only the held task's own cancel is not a failure.
		done, _ := r.m.Store().GetPRD(prd.ID)
		if done.Status != PRDFailed {
			t.Fatalf("Automaton status = %q, want %q after a dependent of the cancelled task failed", done.Status, PRDFailed)
		}
	})

	t.Run("task_max_failures_overrides_global", func(t *testing.T) {
		// The global default allows three failed attempts; the parser task
		// allows four. Only the task-level field differs, so Enabled and the
		// ladder still come from the global policy.
		r := newFlowRig(t, flowGlobalPolicy(), 4)
		prd := r.addFlowPRD(t, func(task *Task) {
			task.FailureEscalation = &FailureEscalationPolicy{MaxFailures: 4}
		})
		parserID := taskByTitle(t, r.m, prd.ID, flowParserTask).ID
		depID := taskByTitle(t, r.m, prd.ID, flowDependentTask).ID

		eff := r.m.resolveFailureEscalation(prd, taskByTitle(t, r.m, prd.ID, flowParserTask))
		if eff.MaxFailures != 4 || eff.Sources.MaxFailures != "task" || eff.Sources.Enabled != "global" || eff.Sources.Steps != "global" {
			t.Fatalf("resolved policy max %d sources %+v, want max 4 from the task and the rest from the global default", eff.MaxFailures, eff.Sources)
		}

		r.run(t, prd)

		if n := r.spawns.spawnsFor(parserID); n != 4 {
			t.Fatalf("parser spawns = %d, want exactly 4 before the hold", n)
		}
		if n := r.spawns.spawnsFor(depID); n != 0 {
			t.Fatalf("dependent spawns = %d, want 0 while the parser is held", n)
		}
		held := taskByTitle(t, r.m, prd.ID, flowParserTask)
		if held.Status != TaskReviewNeeded || held.ReviewSummary == nil || held.ReviewSummary.FailureCount != 4 {
			t.Fatalf("parser status %q summary %+v, want held after 4 failures", held.Status, held.ReviewSummary)
		}
		if n := flowCountAlerts(&r.alerts); n != 1 {
			t.Fatalf("review alerts = %d, want exactly 1", n)
		}
	})
}
