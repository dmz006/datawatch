package server

// Automata event log: error classification.
//
// automataEventErrorClassify turns a raw error string from a named source
// kind into the operator view of that error: what happened (summary), why it
// matters (reason), and what the operator should do next (next_action). A
// permission error that is harmless (a stat of a removed worktree, or a read
// of a path the daemon does not need) is quiet and info, and keeps its reason
// visible. Every other error is severity error with a concrete next action.
// The function is pure, so the operator event log and tests share one rule.
// Operator text says Automaton or Automata, never PRD.

import (
	"strings"
)

// automataErrorSummaryLimit caps the one-line summary so a long raw error
// cannot flood the event list.
const automataErrorSummaryLimit = 160

// AutomataErrorClassification is the operator view of one raw error.
type AutomataErrorClassification struct {
	Summary    string                `json:"summary"`
	Reason     string                `json:"reason"`
	NextAction string                `json:"next_action"`
	Quiet      bool                  `json:"quiet"`
	Severity   AutomataEventSeverity `json:"severity"`
}

// automataErrorPermissionMarkers are the raw-text forms of a permission error.
var automataErrorPermissionMarkers = []string{
	"permission denied",
	"operation not permitted",
	"eacces",
	"eperm",
}

// automataErrorHarmlessMarkers name the permission failures that do not affect
// the Automaton: a stat of a worktree that was already removed, or a read of a
// path the daemon does not need. A denied permission error matching one of
// these is quiet.
var automataErrorHarmlessMarkers = []string{
	"removed worktree",
	"worktree removed",
	"not needed by the daemon",
	"daemon does not need",
}

// automataErrorIsPermission reports whether the raw text is a permission error.
func automataErrorIsPermission(lower string) bool {
	for _, m := range automataErrorPermissionMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// automataErrorIsHarmless reports whether a permission error can be ignored:
// it names a harmless marker, or it is a denied stat of a worktree.
func automataErrorIsHarmless(lower string) bool {
	for _, m := range automataErrorHarmlessMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return strings.Contains(lower, "stat") && strings.Contains(lower, "worktree")
}

// automataErrorShort makes raw error text one line, applies the Automaton
// wording, and caps its length on a rune boundary.
func automataErrorShort(raw string) string {
	s := automataOperatorText(raw)
	r := []rune(s)
	if len(r) > automataErrorSummaryLimit {
		return string(r[:automataErrorSummaryLimit-3]) + "..."
	}
	return s
}

// automataEventErrorClassify classifies one raw error from a source kind.
//
// A harmless permission error is info and quiet, and its reason says why it
// is harmless. A real permission error is error with a next action that names
// the access to grant. An empty error is error, since a source reported
// failure without saying what failed. Any other error is error with a
// next action to open the daemon log and retry.
func automataEventErrorClassify(raw, sourceKind string) AutomataErrorClassification {
	kind := strings.TrimSpace(sourceKind)
	if kind == "" {
		kind = "source"
	}
	text := automataErrorShort(raw)
	lower := strings.ToLower(strings.TrimSpace(raw))

	if lower == "" {
		return AutomataErrorClassification{
			Summary:    automataOperatorText("Error reported by " + kind + " with no message"),
			Reason:     automataOperatorText("The " + kind + " source reported a failure but recorded no error text"),
			NextAction: automataOperatorText("Check the " + kind + " source log and record its error message"),
			Severity:   AutomataSeverityError,
		}
	}

	if automataErrorIsPermission(lower) {
		if automataErrorIsHarmless(lower) {
			return AutomataErrorClassification{
				Summary:  automataOperatorText("Harmless permission error from " + kind + ": " + text),
				Reason:   automataOperatorText("Expected and ignorable: the daemon does not need this path, so the denial does not block any Automaton. Raw error: " + text),
				Quiet:    true,
				Severity: AutomataSeverityInfo,
			}
		}
		return AutomataErrorClassification{
			Summary:    automataOperatorText("Permission denied in " + kind + ": " + text),
			Reason:     automataOperatorText("The daemon user lacks access, so this " + kind + " step cannot finish. Raw error: " + text),
			NextAction: automataOperatorText("Grant the daemon user access to the path in the error, then retry the " + kind + " step"),
			Severity:   AutomataSeverityError,
		}
	}

	return AutomataErrorClassification{
		Summary:    automataOperatorText("Error in " + kind + ": " + text),
		Reason:     automataOperatorText("The " + kind + " step failed with an error that is not a known harmless case. Raw error: " + text),
		NextAction: automataOperatorText("Open the daemon log for the full " + kind + " error, fix its cause, then retry the step"),
		Severity:   AutomataSeverityError,
	}
}
