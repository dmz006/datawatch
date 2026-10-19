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
