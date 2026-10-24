// Failure escalation — attempt records.
//
// Every failed attempt of a task is written to Task.Attempts, so the next
// attempt's prompt and the operator's review can read what went wrong. The
// retry loop in executeOne calls recordFailedAttempt from each branch that
// leads to another attempt or to exhaustion. Records are cumulative: Number
// keeps counting up across operator actions and nothing is ever removed.
// The policy model these records belong to lives in failure_escalation.go.
package autonomous

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"
)

// maxAttemptChangedFiles caps how many changed-file paths one attempt keeps.
const maxAttemptChangedFiles = 50

// Outcomes written to TaskAttempt.Outcome by the retry loop.
const (
	attemptOutcomeFailed  = "failed"
	attemptOutcomeStalled = "stalled"
)

// attemptRun is what the retry loop knows about the attempt it is running:
// the worker it was spawned on, when it started, and how many snapshot refs
// the task held before it began. The refs count lets the record find the
// snapshot this attempt added, if any.
type attemptRun struct {
	Backend    string
	Model      string
	Effort     Effort
	StartedAt  time.Time
	RefsBefore int
	// Escalated is true when an escalation step chose this attempt's worker.
	Escalated bool
}

// attemptFailure is how an attempt ended when it did not succeed.
type attemptFailure struct {
	// Outcome is attemptOutcomeFailed or attemptOutcomeStalled.
	Outcome string
	// Category is the failure category: the verifier's category, or the
	// executor's lastFailureCategory when the failure came from elsewhere.
	Category string
	// Summary is the failure text: the verifier summary, or the error or
	// hint that ended the attempt.
	Summary string
	// Verification is the verifier's result. Zero when the attempt ended
	// before verification ran (a stall, a start failure).
	Verification VerificationResult
	// Requeue is true when the same attempt runs again (a session-start
	// retry). Such a failure is recorded but does not count toward the
	// failure escalation policy's max_failures.
	Requeue bool
}

// recordFailedAttempt appends a TaskAttempt for the attempt that just failed
// and persists the task. The worker falls back to the task's resolved worker
// when the attempt did not spawn one (a resumed session). Changed files come
// from the snapshot this attempt recorded, when it recorded one. A persist
// failure is logged; it never changes the attempt's outcome.
func (m *Manager) recordFailedAttempt(ctx context.Context, prd *PRD, t *Task, run attemptRun, f attemptFailure) {
	ended := time.Now()
	backend, model, effort := run.Backend, run.Model, run.Effort
	if backend == "" {
		story := findStory(prd, t.StoryID)
		auto := m.AutomationForPRD(prd)
		backend, model, _ = resolveTaskExecutionLLM(t, story, prd, auto, m.cfg.ExecutionBackend)
		effort = resolveTaskEffort(t, story, prd, auto)
	}

	started := run.StartedAt
	var ref string
	m.store.WithLock(func() {
		if len(t.AttemptRefs) > run.RefsBefore {
			ref = t.AttemptRefs[len(t.AttemptRefs)-1]
		}
		if started.IsZero() && t.StartedAt != nil {
			started = *t.StartedAt
		}
	})
	if started.IsZero() {
		started = ended
	}

	rec := TaskAttempt{
		Backend:         backend,
		Model:           model,
		Effort:          effort,
		StartedAt:       started,
		EndedAt:         ended,
		DurationSeconds: int(ended.Sub(started) / time.Second),
		Outcome:         f.Outcome,
		FailureCategory: f.Category,
		FailureSummary:  f.Summary,
		VerifierSummary: f.Verification.Summary,
		VerifierIssues:  append([]string(nil), f.Verification.Issues...),
		ChangedFiles:    attemptChangedFiles(ctx, m.resolveTaskProjectDir(prd, t), ref),
		SnapshotRef:     ref,
		Escalated:       run.Escalated,
	}
	m.store.WithLock(func() {
		rec.Number = len(t.Attempts) + 1
		t.Attempts = append(t.Attempts, rec)
		if !f.Requeue {
			t.EscalationFailures++
		}
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: record failed attempt %d: %v", prd.ID, t.ID, rec.Number, err)
	}
}

// attemptOutcomeForError names how an attempt ended from the error that ended
// it: a stall is "stalled", anything else is "failed".
func attemptOutcomeForError(err error) string {
	if errors.Is(err, ErrWorkerStalled) {
		return attemptOutcomeStalled
	}
	return attemptOutcomeFailed
}

// attemptChangedFiles lists the files changed by the attempt snapshot ref, as
// `git diff --name-only <ref>^ <ref>` reports them, capped at
// maxAttemptChangedFiles. It returns nil when ref is empty or git fails. It
// never returns an error: a missing file list must not fail an attempt.
func attemptChangedFiles(ctx context.Context, dir, ref string) []string {
	if ref == "" || dir == "" {
		return nil
	}
	out, err := snapshotGit(ctx, dir, attemptGitEnv(), "diff", "--name-only", ref+"^", ref)
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		files = append(files, line)
		if len(files) == maxAttemptChangedFiles {
			break
		}
	}
	return files
}

// attemptGitEnv is the process environment for git calls made on an attempt's
// behalf, with the terminal multiplexer variables removed so nothing reaches
// the operator's multiplexer server.
func attemptGitEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}
