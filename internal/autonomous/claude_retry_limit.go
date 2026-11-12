package autonomous

import (
	"errors"
	"fmt"
	"strings"
)

// ClaudeRetryAction is what to do with the next attempt of a task.
type ClaudeRetryAction string

const (
	// ClaudeRetryAllow lets the attempt run as planned.
	ClaudeRetryAllow ClaudeRetryAction = "allow"
	// ClaudeRetryRoute sends the attempt to a backend other than Claude.
	ClaudeRetryRoute ClaudeRetryAction = "route"
	// ClaudeRetryHold holds the task with a queue question.
	ClaudeRetryHold ClaudeRetryAction = "hold"
)

// ErrClaudeRetryLimit marks a task held because its Claude retries are used up.
// It is built on the attempt-limit error, so it wraps ErrWorkerStalled too.
var ErrClaudeRetryLimit = errors.New("claude retry limit reached")

// IsClaudeBackend reports whether backend is a Claude backend.
func IsClaudeBackend(backend string) bool {
	return strings.HasPrefix(strings.ToLower(backend), "claude")
}

// ClaudeRetryDecision is the verdict for one attempt.
type ClaudeRetryDecision struct {
	Action  ClaudeRetryAction
	Backend string // target backend when Action is route
	Reason  string
}

// DecideClaudeRetry judges the next attempt of a task. retries is how many
// retries of the task already ran on Claude; the first attempt is not a retry.
// The attempt is allowed while retries < limit. Once retries reach the limit the
// attempt never runs on Claude: it is routed to fallback when one is named,
// otherwise the task is held. A limit of 0 or less, or a non-Claude backend, is
// always allowed.
func DecideClaudeRetry(backend string, isRetry bool, retries, limit int, fallback string) ClaudeRetryDecision {
	if limit <= 0 || !isRetry || !IsClaudeBackend(backend) {
		return ClaudeRetryDecision{Action: ClaudeRetryAllow}
	}
	if retries < limit {
		return ClaudeRetryDecision{Action: ClaudeRetryAllow}
	}
	if fallback != "" && !IsClaudeBackend(fallback) {
		return ClaudeRetryDecision{Action: ClaudeRetryRoute, Backend: fallback,
			Reason: fmt.Sprintf("claude retry limit %d reached (%d retries); routed to %s", limit, retries, fallback)}
	}
	return ClaudeRetryDecision{Action: ClaudeRetryHold,
		Reason: fmt.Sprintf("claude retry limit %d reached (%d retries); no other backend to route to", limit, retries)}
}

// ClaudeRetryLimitError is the error a held task carries. It wraps the attempt
// limit error, so the stall retry path sees it like any attempt-limit stop.
func ClaudeRetryLimitError(taskID, reason string) error {
	return fmt.Errorf("%w: task %s: %w", ErrClaudeRetryLimit, taskID, AttemptLimitError(reason, 0, 0))
}

// CountClaudeRetry records that an attempt of t is about to run on backend. The
// counter moves only for a retry on Claude and is kept per task, so another
// task's retries never count against this one. It returns the new count.
func CountClaudeRetry(t *Task, backend string, isRetry bool) int {
	if t != nil && isRetry && IsClaudeBackend(backend) {
		t.ClaudeRetries++
	}
	if t == nil {
		return 0
	}
	return t.ClaudeRetries
}

// ClaudeRetryHoldQuestion is the queue question text for a held task.
func ClaudeRetryHoldQuestion(t *Task, limit int) string {
	return fmt.Sprintf("Task %q (%s) used its %d Claude retries and no other backend is available. "+
		"Route it to another backend, raise usage.burn_guard.retry_limit_claude, or cancel it?", t.Title, t.ID, limit)
}

// ApplyClaudeRetryLimit applies the daemon-wide retry limit to the next attempt
// of t on backend. It returns the backend to run on and a hold error when the
// task must not run. The caller has already routed by the ceiling. The route
// target is the cheaper backend. A disabled guard is a no-op.
func ApplyClaudeRetryLimit(t *Task, backend string, isRetry bool) (string, error) {
	g := currentBurnGuardLimits()
	if !g.Enabled {
		return backend, nil
	}
	d := DecideClaudeRetry(backend, isRetry, t.ClaudeRetries, g.RetryLimitClaude, cheaperBackendFor)
	switch d.Action {
	case ClaudeRetryRoute:
		return d.Backend, nil
	case ClaudeRetryHold:
		return backend, ClaudeRetryLimitError(t.ID, d.Reason)
	}
	CountClaudeRetry(t, backend, isRetry)
	return backend, nil
}
