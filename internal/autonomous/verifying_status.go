package autonomous

// ResolveVerifyingTask decides the status a task in the verifying state
// should resolve to once its verifier session is no longer (or never was)
// owning it. It is pure: no I/O, no manager access, no side effects.
//
// It returns:
//   - TaskVerifying when the task has a session ID and alive reports that
//     session is still live: a live verifier session still owns the task.
//   - TaskCompleted when the task carries a recorded verification verdict
//     with OK == true: the recorded ok verdict is applied.
//   - TaskPending in every remaining case — a dead or absent session with
//     a recorded failed verdict (OK == false), no recorded verification at
//     all, or a nil alive function: the normal retry path.
func ResolveVerifyingTask(alive func(sessionID string) bool, t *Task) TaskStatus {
	if alive != nil && t.SessionID != "" && alive(t.SessionID) {
		return TaskVerifying
	}
	if t.Verification != nil && t.Verification.OK {
		return TaskCompleted
	}
	return TaskPending
}

// VerifyingResume is the boot-time decision for a task found in the
// verifying state.
type VerifyingResume int

const (
	// VerifyingResumeReset: the worker did not finish (or its verdict failed);
	// the task is reset to pending.
	VerifyingResumeReset VerifyingResume = iota
	// VerifyingResumeKeepLive: a live session still owns the task.
	VerifyingResumeKeepLive
	// VerifyingResumeReverify: the worker finished and no verdict is recorded;
	// keep the task verifying and run the verification once.
	VerifyingResumeReverify
	// VerifyingResumeComplete: a recorded ok verdict stands.
	VerifyingResumeComplete
)

// taskHasWorkerResult reports whether the task carries a recorded worker
// result: the deterministic commit recorded when the worker finished.
func taskHasWorkerResult(t *Task) bool {
	return t != nil && len(t.AttemptCommitSHAs) > 0
}

// ResolveVerifyingResume decides what boot-resume does with a verifying task.
// It is pure: no I/O, no manager access. A live session is kept; a recorded
// ok verdict completes; a finished worker (completion marker in the recorded
// output, or a recorded worker result) with no verdict re-verifies only;
// anything else, including an empty session without a worker result and a
// recorded failed verdict, resets.
func ResolveVerifyingResume(alive func(sessionID string) bool, output func(sessionID string) (string, bool), t *Task) VerifyingResume {
	switch ResolveVerifyingTask(alive, t) {
	case TaskVerifying:
		return VerifyingResumeKeepLive
	case TaskCompleted:
		return VerifyingResumeComplete
	}
	if t.Verification != nil {
		return VerifyingResumeReset // recorded failed verdict: normal retry
	}
	if taskHasWorkerResult(t) {
		return VerifyingResumeReverify
	}
	if t.SessionID != "" && output != nil {
		if out, ok := output(t.SessionID); ok && outputHasCompletionMarker(out) {
			return VerifyingResumeReverify
		}
	}
	return VerifyingResumeReset
}
