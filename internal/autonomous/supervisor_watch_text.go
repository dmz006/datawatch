// Supervisor watch text — the operator-facing one-liner per finding
// kind, the single place the wording of every finding is written.
//
// Every line is exactly one line, names the Automaton or Automata the
// finding is about wherever a work item is meant, and never contains
// the word PRD or a tracker id: the operator reads these in alerts and
// the record store, not in the internal tracking sheet.

package autonomous

import (
	"fmt"
	"strings"
)

// SupervisorWatchText returns the operator-facing one-line text for one
// finding kind, with subject inlined (the stable id the finding is
// about: an Automaton id, a node name, a lane name, a CI run id, or the
// main checkout's path). An unknown kind is an error so a typo in a
// kind never produces a finding that reads like nothing happened; a
// blank subject is rendered as "the subject" so the line still reads
// as one line of English. The result always contains exactly one line
// of text, never the word PRD, and never a tracker id.
func SupervisorWatchText(kind SupervisorWatchKind, subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "the subject"
	}
	switch kind {
	case SupervisorWatchTmuxRestart:
		return fmt.Sprintf("tmux restarted on %s: audit every non-terminal Automaton there", subject), nil
	case SupervisorWatchDaemonInactive:
		return fmt.Sprintf("the Automaton daemon is not active on %s", subject), nil
	case SupervisorWatchModelNotResident:
		return fmt.Sprintf("the working model is not resident on %s: reload it before routing more Automata", subject), nil
	case SupervisorWatchTaskFailed:
		return fmt.Sprintf("a task in Automaton %s failed: read the verifier verdict before retrying", subject), nil
	case SupervisorWatchTaskReviewNeeded:
		return fmt.Sprintf("a task in Automaton %s is waiting for review", subject), nil
	case SupervisorWatchAutomatonNotMerged:
		return fmt.Sprintf("Automaton %s finished all of its work but is not merged", subject), nil
	case SupervisorWatchIdleLaneEligibleWork:
		return fmt.Sprintf("lane %s is idle while Automata work that is eligible to run is waiting", subject), nil
	case SupervisorWatchWorktreeNoGomod:
		return fmt.Sprintf("the worktree of Automaton %s has no go.mod: its project directory is broken", subject), nil
	case SupervisorWatchWorktreeNotRegistered:
		return fmt.Sprintf("the worktree of Automaton %s is not registered", subject), nil
	case SupervisorWatchWorktreeSilent:
		return fmt.Sprintf("Automaton %s went silent while its worker was expected to be active: check it before re-dispatching", subject), nil
	case SupervisorWatchVerifyScriptNotExecutable:
		return fmt.Sprintf("the verify script of Automaton %s is not executable: run its check manually", subject), nil
	case SupervisorWatchMainCheckoutDirty:
		return fmt.Sprintf("the main checkout %s has uncommitted tracked files: any Automaton ship will abort until they are committed", subject), nil
	case SupervisorWatchCIRed:
		return fmt.Sprintf("CI run %s is red: triage the failure before shipping more Automata", subject), nil
	case SupervisorWatchCISTuck:
		return fmt.Sprintf("CI run %s has been in progress abnormally long: the Automata release pipeline is stalled until it is done", subject), nil
	default:
		return "", fmt.Errorf("supervisor watch: no text for unknown kind %q (must be one of %s)",
			kind, strings.Join(supervisorWatchKindStrings(), ", "))
	}
}

// supervisorWatchUnverifiedNoteText is the fixed clause added to a
// finding raised about a node that was probed with certificate
// verification turned off.
const supervisorWatchUnverifiedNoteText = "certificate verification is turned off for this node"

// SupervisorWatchUnverifiedNote appends the fixed unverified-node clause
// to a finding text, keeping it one line.
func SupervisorWatchUnverifiedNote(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return supervisorWatchUnverifiedNoteText
	}
	return text + "; " + supervisorWatchUnverifiedNoteText
}
