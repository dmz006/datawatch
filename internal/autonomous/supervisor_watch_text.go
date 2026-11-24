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

// SupervisorWatchBacklogSubject is the fixed "backlog" subject every
// backlog finding carries, so the kind+subject pair dedupes the
// consolidated finding the way one Automaton's task_failed findings do.
const SupervisorWatchBacklogSubject = "backlog"

// SupervisorWatchPriorityStallText builds the operator line for one
// priority-stall finding: it names the Automaton, its resolved priority,
// how long it has been waiting, the stall threshold and the floor, and
// says it is not being picked up. One line, the Automaton name, never a
// tracker id.
//
// blockerText and action, when both are non-empty, extend the line with the
// same blocker and action text the priority_stall wake carries; empty values
// give the line without them.
func SupervisorWatchPriorityStallText(subject string, priority, ageMinutes, thresholdMinutes, floor int, blockerText, action string) string {
	s := fmt.Sprintf(
		"Automaton %s at priority %d has been waiting %d minutes (stall threshold %d minutes, priority floor %d) without being picked up",
		subject, priority, ageMinutes, thresholdMinutes, floor)
	if blockerText != "" && action != "" {
		s += fmt.Sprintf(": %s. To clear it: %s", blockerText, action)
	}
	return s
}

// SupervisorWatchBacklogText builds the operator line for the
// consolidated backlog finding. count is the total number of listed
// Automata and entries is the already-prepared entry list — sorted by
// highest resolved priority first, within the same priority by oldest
// created first, and already capped at sixteen entries with a "+k more"
// tail — joined into the one-line text that starts with
// "BACKLOG <n> Automata (highest priority first, then oldest)", the
// Automata name, never a tracker id.
func SupervisorWatchBacklogText(count int, entries []string) string {
	return fmt.Sprintf(
		"BACKLOG %d Automata (highest priority first, then oldest): %s",
		count, strings.Join(entries, " "))
}

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
	case SupervisorWatchArchivedUnmerged:
		// The check-side text (SupervisorWatchArchivedUnmergedText) carries
		// the title, branch and ahead count; this fallback names the remedy.
		return fmt.Sprintf("archived Automaton %s has commits on its branch that are not on the base: carry it forward to keep the work", subject), nil
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
	case SupervisorWatchMainCheckoutAutomaton:
		return fmt.Sprintf("Automaton %s is open while its project directory is the main checkout: recreate it without a project directory so it runs isolated in its own worktree", subject), nil
	case SupervisorWatchCIRed:
		return fmt.Sprintf("CI run %s is red: triage the failure before shipping more Automata", subject), nil
	case SupervisorWatchCISTuck:
		return fmt.Sprintf("CI run %s has been in progress abnormally long: the Automata release pipeline is stalled until it is done", subject), nil

	case SupervisorWatchPriorityStall:
		// The check-side text (SupervisorWatchPriorityStallText) carries
		// the priority, age, threshold and floor; this fallback says the
		// same thing without the numbers.
		return "Automaton " + subject + " is not being picked up: it has been waiting past the stall threshold for its priority", nil

	case SupervisorWatchBacklog:
		// The check-side text (SupervisorWatchBacklogText) carries the
		// sorted, capped entry list; this fallback names the queue without
		// it.
		return "BACKLOG: Automata are waiting for the operator's attention or a free lane, highest priority first, then oldest", nil

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

// SupervisorWatchArchivedUnmergedText is the one-line text of an
// archived_unmerged finding: the Automaton title and id, its branch, how many
// commits it is ahead of the base, and the carry-forward remedy.
func SupervisorWatchArchivedUnmergedText(id, title, branch string, ahead int) string {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = id
	}
	return fmt.Sprintf("archived Automaton %q (%s) has branch %s with %d commits not on the base: run carry-forward %s to start a new draft on that branch",
		title, id, branch, ahead, id)
}
