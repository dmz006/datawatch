package autonomous

import "fmt"

// AttemptLimitError returns an error that wraps ErrWorkerStalled, so the
// executor's stall retry path handles a task-level attempt-limit stop (the
// task_max_attempt_seconds / task_no_progress_seconds guardrails) exactly
// like an SSE or no-output stall: it consumes the normal auto_fix_retries
// budget instead of failing the task on the first occurrence.
func AttemptLimitError(reason string, maxAttemptSeconds, noProgressSeconds int) error {
	return fmt.Errorf("task attempt stopped: %s (task_max_attempt_seconds=%ds, task_no_progress_seconds=%ds): %w",
		reason, maxAttemptSeconds, noProgressSeconds, ErrWorkerStalled)
}
