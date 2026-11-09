// Attempt backend reason — why a task attempt runs on the backend it runs on.
//
// Every attempt record carries BackendReason, a short sentence the operator
// reads in the review report and the Automaton status to see which level chose
// the attempt's worker. describeAttemptBackendReason maps the executor source
// that resolved the worker onto that sentence. The executor sets it on attemptRun
// once the worker is final, and recordFailedAttempt copies it onto the record.
package autonomous

import "fmt"

// Reason texts written to TaskAttempt.BackendReason for the sources that have
// no text of their own. The first-attempt and escalation texts are built from
// the task kind and the failure count.
const (
	attemptReasonOperatorPinned = "operator pinned"
	attemptReasonDefault        = "default choice"
	// attemptReasonLadderStep is used for an escalation step that applies before
	// any attempt failed (a ladder whose first step has a backend).
	attemptReasonLadderStep = "escalation ladder step"
)

// describeAttemptBackendReason returns the BackendReason for an attempt whose
// worker was resolved from source. kind is the task's attempt kind (see
// taskAttemptKind) and failures the number of failed attempts recorded before
// this one. The result is one of:
//
//   - "first attempt for <kind> tasks" when the first-attempt pair applied,
//   - "escalated after <n> failures" (or "1 failure") when an escalation step applied,
//   - "operator pinned" when a task, story, PRD or Automaton pinned the worker,
//   - "default choice" for anything else, including a burn-guard route.
func describeAttemptBackendReason(source, kind string, failures int) string {
	switch source {
	case ExecutorSourceFirstAttempt:
		return fmt.Sprintf("first attempt for %s tasks", kind)
	case ExecutorSourceEscalation:
		switch {
		case failures < 1:
			return attemptReasonLadderStep
		case failures == 1:
			return "escalated after 1 failure"
		}
		return fmt.Sprintf("escalated after %d failures", failures)
	case ExecutorSourceTask, ExecutorSourceStory, ExecutorSourcePRD, ExecutorSourceAutomation:
		return attemptReasonOperatorPinned
	}
	return attemptReasonDefault
}
