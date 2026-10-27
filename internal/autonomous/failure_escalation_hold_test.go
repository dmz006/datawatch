package autonomous

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// holdAlertLog records every operator alert a test run raises.
type holdAlertLog struct {
	mu     sync.Mutex
	alerts []holdAlert
}

type holdAlert struct {
	level, title, body string
}

func (l *holdAlertLog) sink(level, title, body string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.alerts = append(l.alerts, holdAlert{level, title, body})
	return ""
}

func (l *holdAlertLog) all() []holdAlert {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]holdAlert(nil), l.alerts...)
}

// holdVerifyLog records the title of every task the verifier saw.
type holdVerifyLog struct {
	mu     sync.Mutex
	titles []string
}

// verify fails every attempt of the task titled failTitle and passes the
// rest. The failing attempt carries a summary and one issue, so the review
// report has a verifier report to show.
func (l *holdVerifyLog) verify(failTitle string) VerifyFn {
	return func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		l.mu.Lock()
		l.titles = append(l.titles, task.Title)
		l.mu.Unlock()
		if task.Title == failTitle {
			return VerificationResult{
				OK:      false,
				Summary: "tests still fail",
				Issues:  []string{"TestParse: expected 3 tokens, got 2"},
			}, nil
		}
		return VerificationResult{OK: true, Summary: "tests pass"}, nil
	}
}

func (l *holdVerifyLog) count(title string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, t := range l.titles {
		if t == title {
			n++
		}
	}
	return n
}

// newHoldPRD creates an approved Automaton whose single story holds tasks as
// given. Task IDs are taken from the tasks, so dependencies can name them.
// adjust, when non-nil, edits the PRD before approval.
func newHoldPRD(t *testing.T, m *Manager, tasks []Task, adjust func(*PRD)) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "Story", Tasks: tasks}}); err != nil {
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

// taskByTitle returns the stored task of prdID with the given title.
func taskByTitle(t *testing.T, m *Manager, prdID, title string) *Task {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s not found", prdID)
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].Title == title {
				return &prd.Story[si].Tasks[ti]
			}
		}
	}
	t.Fatalf("task %q not found in PRD %s", title, prdID)
	return nil
}

// spawnsFor counts the spawn requests the log recorded for one task.
func (l *escalationSpawnLog) spawnsFor(taskID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, r := range l.reqs {
		if r.TaskID == taskID {
			n++
		}
	}
	return n
}

// TestFailureEscalation_HoldAtConfiguredCount: a task whose every attempt fails
// runs exactly MaxFailures times, then is held in review_needed. The Error
// line names the count, and the review report lists every attempt with its
// worker, duration, outcome and verifier report.
func TestFailureEscalation_HoldAtConfiguredCount(t *testing.T) {
	for _, max := range []int{3, 4} {
		t.Run(fmt.Sprintf("max_failures_%d", max), func(t *testing.T) {
			m, api := newFailureEscalationFixture(t, t.TempDir())
			prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
				p.FailureEscalation = &FailureEscalationPolicy{MaxFailures: max}
			})
			var spawns escalationSpawnLog
			var vlog holdVerifyLog
			runEscalationPRD(t, m, api, prd, &spawns, vlog.verify("Task 1"))

			if n := len(spawns.workers()); n != max {
				t.Fatalf("spawn count = %d, want exactly %d", n, max)
			}
			task := escalationTask(t, m, prd.ID)
			if task.Status != TaskReviewNeeded {
				t.Fatalf("task status = %q, want %q", task.Status, TaskReviewNeeded)
			}
			wantErr := fmt.Sprintf("held for operator review after %d failed attempts", max)
			if task.Error != wantErr {
				t.Fatalf("task.Error = %q, want %q", task.Error, wantErr)
			}
			if task.ReviewSummary == nil {
				t.Fatal("ReviewSummary is nil on a held task")
			}
			if task.ReviewSummary.FailureCount != max {
				t.Fatalf("ReviewSummary.FailureCount = %d, want %d", task.ReviewSummary.FailureCount, max)
			}
			text := task.ReviewSummary.Text
			for i := 1; i <= max; i++ {
				if !strings.Contains(text, fmt.Sprintf("Attempt %d\n", i)) {
					t.Fatalf("review report misses attempt %d:\n%s", i, text)
				}
			}
			for _, want := range []string{"Worker: ", "Duration: ", "Outcome: failed", "Verifier summary: tests still fail", "- TestParse: expected 3 tokens, got 2", "Files changed:"} {
				if !strings.Contains(text, want) {
					t.Fatalf("review report misses %q:\n%s", want, text)
				}
			}
		})
	}
}

// TestFailureEscalation_HoldRaisesOneAlert: holding a task raises exactly one
// error alert that names the Automaton, the task, the failure count, the last
// worker and the ways to act.
func TestFailureEscalation_HoldRaisesOneAlert(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	var alerts holdAlertLog
	m.SetAlertSink(alerts.sink)
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)

	got := alerts.all()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want exactly 1: %+v", len(got), got)
	}
	a := got[0]
	if a.level != "error" || a.title != "Automaton task needs review" {
		t.Fatalf("alert level/title = %q/%q, want error/Automaton task needs review", a.level, a.title)
	}
	task := escalationTask(t, m, prd.ID)
	for _, want := range []string{
		"Automaton: " + prd.ID,
		"Task: " + task.ID + " (" + task.Title + ")",
		fmt.Sprintf("Failed attempts: %d", defaultFailureEscalationMaxFailures),
		"Last worker: ",
		"retry the task",
		"change its spec",
		"change its worker",
		"narrow its scope",
		"cancel it",
	} {
		if !strings.Contains(a.body, want) {
			t.Fatalf("alert body misses %q:\n%s", want, a.body)
		}
	}
	if strings.Contains(a.body, "PRD") {
		t.Fatalf("alert body uses the internal term PRD:\n%s", a.body)
	}
}

// TestFailureEscalation_HeldTaskBlocksDependentsNotSiblings: a held task keeps
// its dependent from running, but an independent sibling still runs to
// completion. Both the sequential and the concurrent coordinator behave so.
func TestFailureEscalation_HeldTaskBlocksDependentsNotSiblings(t *testing.T) {
	for _, conc := range []int{1, 2} {
		t.Run(fmt.Sprintf("concurrency_%d", conc), func(t *testing.T) {
			m, api := newFailureEscalationFixture(t, t.TempDir())
			// Each task gets its own tree: tasks sharing one tree queue on its
			// write lock, which would make the concurrent run wait on the lock.
			prd := newHoldPRD(t, m, []Task{
				{Title: "Held task", Spec: "never passes", ProjectDir: t.TempDir()},
				{Title: "Dependent task", Spec: "needs the held one", DependsOn: []string{"Held task"}, ProjectDir: t.TempDir()},
				{Title: "Sibling task", Spec: "independent", ProjectDir: t.TempDir()},
			}, func(p *PRD) { p.MaxConcurrentTasks = conc })
			depID := taskByTitle(t, m, prd.ID, "Dependent task").ID
			var spawns escalationSpawnLog
			var vlog holdVerifyLog
			runEscalationPRD(t, m, api, prd, &spawns, vlog.verify("Held task"))

			if s := taskByTitle(t, m, prd.ID, "Held task"); s.Status != TaskReviewNeeded {
				t.Fatalf("held task status = %q, want %q", s.Status, TaskReviewNeeded)
			}
			if s := taskByTitle(t, m, prd.ID, "Sibling task"); s.Status != TaskCompleted {
				t.Fatalf("sibling status = %q, want %q: independent work must keep running", s.Status, TaskCompleted)
			}
			dep := taskByTitle(t, m, prd.ID, "Dependent task")
			if normTaskStatus(dep.Status) != TaskPending {
				t.Fatalf("dependent status = %q, want %q (it waits, never fails)", dep.Status, TaskPending)
			}
			if n := spawns.spawnsFor(depID); n != 0 {
				t.Fatalf("dependent was spawned %d times, want 0", n)
			}
			if n := vlog.count("Dependent task"); n != 0 {
				t.Fatalf("dependent was verified %d times, want 0", n)
			}
		})
	}
}

// TestFailureEscalation_HeldTaskKeepsAutomatonOpen: a held task leaves the
// Automaton blocked, with the reason naming the task. It is neither completed
// nor failed, and the roll-up does not touch it.
func TestFailureEscalation_HeldTaskKeepsAutomatonOpen(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDBlocked {
		t.Fatalf("Automaton status = %q, want %q", got.Status, PRDBlocked)
	}
	task := escalationTask(t, m, prd.ID)
	want := "task " + task.ID + " needs operator review"
	if got.BlockedReason != want {
		t.Fatalf("BlockedReason = %q, want %q", got.BlockedReason, want)
	}
	if task.Status == TaskFailed || task.Status == TaskCompleted {
		t.Fatalf("held task status = %q, want it neither failed nor completed", task.Status)
	}
}

// TestFailureEscalation_BootReconcileLeavesHeldTask: the boot-time reset of
// stale in-progress and verifying tasks never touches a held task, and an
// Automaton that is resumed and run again leaves the held task held without
// spawning a worker for it.
func TestFailureEscalation_BootReconcileLeavesHeldTask(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)

	held := escalationTask(t, m, prd.ID)
	if held.Status != TaskReviewNeeded {
		t.Fatalf("setup: task status = %q, want %q", held.Status, TaskReviewNeeded)
	}
	sessionBefore := held.SessionID
	errBefore := held.Error

	stored, _ := m.Store().GetPRD(prd.ID)
	m.resetInProgressTasksForResume(stored)
	m.resetKilledTasksForResume(stored)
	m.reconcileStuckTasks()

	after := escalationTask(t, m, prd.ID)
	if after.Status != TaskReviewNeeded {
		t.Fatalf("boot reconcile changed held task to %q, want %q", after.Status, TaskReviewNeeded)
	}
	if after.SessionID != sessionBefore || after.Error != errBefore {
		t.Fatalf("boot reconcile touched a held task: session %q->%q error %q->%q",
			sessionBefore, after.SessionID, errBefore, after.Error)
	}

	// A resumed Automaton is run again: the held task must not spawn.
	before := len(spawns.workers())
	// Let the first run goroutine finish before the stored PRD is touched.
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	m.mu.Lock()
	m.Store().WithLock(func() { cur.Status = PRDApproved })
	m.mu.Unlock()
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)
	if n := len(spawns.workers()) - before; n != 0 {
		t.Fatalf("re-run spawned %d workers for a held task, want 0", n)
	}
	if s := escalationTask(t, m, prd.ID); s.Status != TaskReviewNeeded {
		t.Fatalf("re-run changed held task to %q, want %q", s.Status, TaskReviewNeeded)
	}
}

// TestFailureEscalation_DisabledStillFailsTask: with escalation off the legacy
// path runs: the task is marked failed after the retry budget, the Automaton
// fails, and no review alert is raised.
func TestFailureEscalation_DisabledStillFailsTask(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	var alerts holdAlertLog
	m.SetAlertSink(alerts.sink)
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{Enabled: escalationOn(false)}
	})
	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)

	task := escalationTask(t, m, prd.ID)
	if task.Status != TaskFailed {
		t.Fatalf("task status = %q, want %q", task.Status, TaskFailed)
	}
	if !strings.HasPrefix(task.Error, "verification failed after retries") {
		t.Fatalf("task.Error = %q, want the legacy exhaustion message", task.Error)
	}
	if task.ReviewSummary != nil {
		t.Fatalf("ReviewSummary set with escalation disabled: %+v", task.ReviewSummary)
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDFailed {
		t.Fatalf("Automaton status = %q, want %q", got.Status, PRDFailed)
	}
	if n := len(alerts.all()); n != 0 {
		t.Fatalf("alerts = %d, want 0 with escalation disabled", n)
	}
}

// TestFailureEscalation_TaskOverrideMaxFailures: a task's own MaxFailures beats
// the Automaton's. Without a task override the Automaton's value applies.
func TestFailureEscalation_TaskOverrideMaxFailures(t *testing.T) {
	tests := []struct {
		name     string
		taskMax  int
		wantRuns int
	}{
		{"task override wins", 2, 2},
		{"automaton value applies without a task override", 0, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, api := newFailureEscalationFixture(t, t.TempDir())
			prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
				p.FailureEscalation = &FailureEscalationPolicy{MaxFailures: 4}
				if tc.taskMax > 0 {
					p.Story[0].Tasks[0].FailureEscalation = &FailureEscalationPolicy{MaxFailures: tc.taskMax}
				}
			})
			var spawns escalationSpawnLog
			runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)
			if n := len(spawns.workers()); n != tc.wantRuns {
				t.Fatalf("spawn count = %d, want %d", n, tc.wantRuns)
			}
			if s := escalationTask(t, m, prd.ID); s.Status != TaskReviewNeeded {
				t.Fatalf("task status = %q, want %q", s.Status, TaskReviewNeeded)
			}
		})
	}
}

// TestFailureEscalation_HoldRecordsLearningOnce: the failure is written to the
// learnings store once, when the task is held, not once per attempt.
func TestFailureEscalation_HoldRecordsLearningOnce(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)

	task := escalationTask(t, m, prd.ID)
	n := 0
	for _, l := range m.Store().ListLearnings() {
		if l.TaskID == task.ID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("learnings for held task = %d, want 1", n)
	}
}

// TestFailureEscalation_HoldStatusCountedInSummary: the Automaton summary counts
// held tasks under review_needed, so list and status views show them.
func TestFailureEscalation_HoldStatusCountedInSummary(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)

	got, _ := m.Store().GetPRD(prd.ID)
	view := BuildSummaryView(got)
	if n := view.TaskStatusCounts[string(TaskReviewNeeded)]; n != 1 {
		t.Fatalf("TaskStatusCounts[review_needed] = %d, want 1: %v", n, view.TaskStatusCounts)
	}
}

// TestFailureEscalation_ReviewSummaryListsEveryAttempt: the report lists every
// recorded attempt with its worker, duration, outcome, changed files and
// verifier report, then the total time, and it stays within its size bound.
func TestFailureEscalation_ReviewSummaryListsEveryAttempt(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	task := &Task{
		ID: "task-x", Title: "Parse input", EscalationFailures: 2,
		Attempts: []TaskAttempt{
			{
				Number: 1, Backend: escClaudeBackend, Model: escClaudeModel,
				DurationSeconds: 30, Outcome: "failed", FailureCategory: "verification",
				VerifierSummary: "build passes, tests fail", VerifierIssues: []string{"TestA failed"},
				ChangedFiles: []string{"parse.go", "parse_test.go"},
			},
			{
				Number: 2, Backend: escPiBackend, Model: escPiModel,
				DurationSeconds: 45, Outcome: "stalled",
				FailureSummary: "worker stalled", ChangedFiles: []string{"lexer.go"},
			},
		},
	}
	rs := buildReviewSummary(task, now)
	if rs.FailureCount != 2 || rs.TotalSeconds != 75 || !rs.HeldAt.Equal(now) {
		t.Fatalf("summary = count %d total %d held %v, want 2, 75, %v", rs.FailureCount, rs.TotalSeconds, rs.HeldAt, now)
	}
	for _, want := range []string{
		"Attempt 1\n", "Worker: " + escClaudeBackend + "/" + escClaudeModel, "Duration: 30 s",
		"Outcome: failed (category verification)", "Verifier summary: build passes, tests fail",
		"- TestA failed", "- parse.go", "- parse_test.go",
		"Attempt 2\n", "Worker: " + escPiBackend + "/" + escPiModel, "Duration: 45 s",
		"Outcome: stalled", "- lexer.go",
		"Total time spent: 75 s across 2 recorded attempts.",
	} {
		if !strings.Contains(rs.Text, want) {
			t.Fatalf("review report misses %q:\n%s", want, rs.Text)
		}
	}

	long := &Task{ID: "task-long", EscalationFailures: 40}
	for i := 1; i <= 40; i++ {
		long.Attempts = append(long.Attempts, TaskAttempt{
			Number: i, Outcome: "failed", VerifierSummary: strings.Repeat("x", 3000),
		})
	}
	if got := buildReviewSummary(long, now); len(got.Text) > reviewSummaryMaxChars {
		t.Fatalf("review report is %d bytes, want at most %d", len(got.Text), reviewSummaryMaxChars)
	}
}

// legacyFailureConfig returns the default config with failure escalation off,
// for tests that pin the legacy task-failed outcome of an exhausted budget.
func legacyFailureConfig() Config {
	cfg := DefaultConfig()
	cfg.FailureEscalation = FailureEscalationPolicy{Enabled: escalationOn(false)}
	return cfg
}
