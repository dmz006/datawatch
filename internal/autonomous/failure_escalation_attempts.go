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
	// attemptOutcomeCancelled is an attempt cut short by an operator
	// cancellation or a daemon restart.
	attemptOutcomeCancelled = "cancelled"
)

// End reasons written to TaskAttempt.EndReason. The retry loop names the
// reason at each place it closes an attempt; recordFailedAttempt derives one
// from the outcome and category when a caller leaves it empty.
const (
	attemptEndCompleted           = "completed"
	attemptEndVerifierFailed      = "verifier_failed"
	attemptEndStalled             = "stalled"
	attemptEndSessionDied         = "session_died"
	attemptEndDaemonRestart       = "daemon_restart"
	attemptEndLaunchRefused       = "launch_refused"
	attemptEndAdoptedThenFailed   = "adopted_then_failed"
	attemptEndCancelledByOperator = "cancelled_by_operator"
	// attemptEndExecutorError is a failure that no verifier, stall or session
	// fault explains (a plain executor error).
	attemptEndExecutorError = "executor_error"
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
	// SessionID and TmuxSession name the session the attempt used, when the
	// caller knows them. Both stay empty when no session started.
	SessionID   string
	TmuxSession string
	// Adopted is true when the attempt ran on a session adopted after a
	// restart rather than one this attempt spawned.
	Adopted bool
	// AttemptsBefore is len(t.Attempts) when the attempt began; the executor's
	// exit hook uses it to tell whether the attempt was already recorded.
	AttemptsBefore int
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
	// EndReason is the machine value for why the attempt ended (an
	// attemptEnd* constant) and EndDetail its free text. EndReason empty
	// means "derive it from Outcome and Category".
	EndReason string
	EndDetail string
}

// deriveAttemptEndReason picks an end reason for a failure whose caller did
// not name one.
func deriveAttemptEndReason(f attemptFailure, adopted bool) string {
	switch {
	case f.Verification.EndReason == attemptEndVerifierMaxRun:
		// A run-cap end keeps its own name on an adopted session too.
		return attemptEndVerifierMaxRun
	case f.EndReason != "":
		// A failure on an adopted session is labelled as such; the reasons that
		// say the attempt was cut short or never started keep their own name.
		if adopted && (f.EndReason == attemptEndVerifierFailed || f.EndReason == attemptEndStalled ||
			f.EndReason == attemptEndSessionDied || f.EndReason == attemptEndExecutorError) {
			return attemptEndAdoptedThenFailed
		}
		return f.EndReason
	case adopted:
		return attemptEndAdoptedThenFailed
	case f.Outcome == attemptOutcomeStalled:
		return attemptEndStalled
	case f.Outcome == attemptOutcomeCancelled:
		return attemptEndCancelledByOperator
	case f.Category == "infra":
		return attemptEndSessionDied
	case f.Verification.Summary != "" || len(f.Verification.Issues) > 0:
		return attemptEndVerifierFailed
	}
	return attemptEndExecutorError
}

// attemptNow returns the time an attempt starts or ends at: the injected
// attempt clock when one is set, else the wall clock.
func (m *Manager) attemptNow() time.Time {
	if m.attemptClock != nil {
		return m.attemptClock()
	}
	return time.Now()
}

// recordFailedAttempt appends a TaskAttempt for the attempt that just failed
// and persists the task. The worker falls back to the task's resolved worker
// when the attempt did not spawn one (a resumed session). Changed files come
// from the snapshot this attempt recorded, when it recorded one. A persist
// failure is logged; it never changes the attempt's outcome.
func (m *Manager) recordFailedAttempt(ctx context.Context, prd *PRD, t *Task, run attemptRun, f attemptFailure) {
	ended := m.attemptNow()
	backend, model, effort := run.Backend, run.Model, run.Effort
	if backend == "" {
		story := findStory(prd, t.StoryID)
		auto := m.AutomationForPRD(prd)
		backend, model, _ = resolveTaskExecutionLLM(t, story, prd, auto, m.cfg.ExecutionBackend)
		effort = resolveTaskEffort(t, story, prd, auto)
	}

	started := run.StartedAt
	var ref string
	sessionID, adopted := run.SessionID, run.Adopted
	tmux := run.TmuxSession
	if tmux == "" && sessionID != "" {
		tmux = m.tmuxSessionFor(sessionID)
	}
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
		SessionID:       sessionID,
		TmuxSession:     tmux,
		EndReason:       deriveAttemptEndReason(f, adopted),
		EndDetail:       f.EndDetail,
	}
	m.store.WithLock(func() {
		if ev := t.RunnerEvidence; ev != nil {
			if ev.ExitStatus != nil {
				status := *ev.ExitStatus
				rec.RunnerExitStatus = &status
			}
			rec.RunnerLastOutput = append([]string(nil), ev.LastOutput...)
			rec.RunnerProcesses = append([]string(nil), ev.Processes...)
			t.RunnerEvidence = nil
		}
		if k := t.CeilingKill; k != nil {
			rec.MemoryKilled, rec.PeakMemoryMB, rec.CeilingMB = true, k.PeakMB, k.CeilingMB
			rec.KilledProcess = k.Process
			if rec.KilledProcess == "" {
				rec.KilledProcess = "unknown"
			}
			t.CeilingKill = nil
		}
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

// tmuxSessionFor returns the tmux session name of the session id, read through
// the injected session lister. It returns "" when no lister is wired or the
// session is unknown.
func (m *Manager) tmuxSessionFor(id string) string {
	m.mu.Lock()
	listFn := m.sessionListerFn
	m.mu.Unlock()
	if listFn == nil || id == "" {
		return ""
	}
	for _, s := range listFn() {
		if s.ID == id {
			return s.TmuxSession
		}
	}
	return ""
}

// recordInterruptedAttempt closes the attempt a cancelled executor left open.
// An operator cancellation (the Automaton or the task is cancelled) is labelled
// cancelled_by_operator; any other cancellation of the executor context is a
// daemon restart. It records onto the stored task, so a status the cancel set
// is not overwritten, and never counts toward the escalation budget.
func (m *Manager) recordInterruptedAttempt(prd *PRD, t *Task, run attemptRun) {
	if run.SessionID == "" || len(t.Attempts) != run.AttemptsBefore {
		return
	}
	reason, detail := attemptEndDaemonRestart, "executor stopped while the attempt was running"
	cur := t
	if latest, ok := m.store.GetPRD(prd.ID); ok && latest != nil {
		if lt, _ := findTaskInPRD(latest, t.ID); lt != nil {
			cur = lt
		}
		if latest.Status == PRDCancelled || (cur != nil && cur.Status == TaskCancelled) {
			reason, detail = attemptEndCancelledByOperator, "cancelled by the operator while the attempt was running"
		}
	}
	if cur.Status == TaskCompleted || len(cur.Attempts) != run.AttemptsBefore {
		return
	}
	m.recordFailedAttempt(context.Background(), prd, cur, run, attemptFailure{
		Outcome: attemptOutcomeCancelled, Summary: detail, Requeue: true, EndReason: reason, EndDetail: detail,
	})
}
