// Uncommitted-work read text: the CLI, the MCP result and the comm channel
// render the body of GET /api/autonomous/uncommitted-work through this file,
// so no surface shows a different wording.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
)

// uncommittedTextFiles caps the files named on one line of the text; the
// rest are counted.
const uncommittedTextFiles = 10

// ParseUncommittedWorkStatus reads the body of GET /api/autonomous/uncommitted-work.
func ParseUncommittedWorkStatus(body []byte) (UncommittedWorkStatus, error) {
	var st UncommittedWorkStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return UncommittedWorkStatus{}, fmt.Errorf("reading the Automaton uncommitted-work status: %w", err)
	}
	if st.Held == nil {
		st.Held = []UncommittedWorkEntry{}
	}
	return st, nil
}

// RenderUncommittedWorkStatus formats the uncommitted-work status read: the
// policy and scope in effect, then one block per held task or Automaton with
// the files, the files outside the task's declared scope, the reason and the
// question that waits for an answer. It only describes — the operator answers
// the question (commit, discard or retry) to release a hold.
func RenderUncommittedWorkStatus(st UncommittedWorkStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton uncommitted work (policy: %s, scope: %s):\n", st.Policy, st.Scope)
	if len(st.Held) == 0 {
		b.WriteString("  Nothing is held: no Automaton has uncommitted work waiting.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  %d held:\n", len(st.Held))
	for _, e := range st.Held {
		b.WriteString("  - " + uncommittedEntryHeading(e) + "\n")
		if len(e.Files) > 0 {
			b.WriteString("      files: " + boundedFileList(e.Files, uncommittedTextFiles) + "\n")
		}
		if len(e.OutsideScope) > 0 {
			b.WriteString("      outside the task's declared files: " + boundedFileList(e.OutsideScope, uncommittedTextFiles) + "\n")
		}
		if e.Reason != "" {
			b.WriteString("      reason: " + e.Reason + "\n")
		}
		if !e.HeldAt.IsZero() {
			b.WriteString("      held since: " + e.HeldAt.Format("2006-01-02 15:04:05 MST") + "\n")
		}
		if e.QuestionID != "" {
			b.WriteString("      question: " + e.QuestionID + " (answer commit, discard or retry)\n")
		}
	}
	b.WriteString("This is a status read only: answer the question to release a hold.\n")
	return b.String()
}

// uncommittedEntryHeading names what a hold is on: a task of an Automaton or
// the whole Automaton worktree.
func uncommittedEntryHeading(e UncommittedWorkEntry) string {
	who := e.Name
	if who == "" {
		who = e.AutomatonID
	}
	if e.TaskID != "" {
		title := e.TaskTitle
		if title == "" {
			title = e.TaskID
		}
		return fmt.Sprintf("Automaton %s (%s), task %s (%s)", who, e.AutomatonID, title, e.TaskID)
	}
	return fmt.Sprintf("Automaton %s (%s), whole worktree", who, e.AutomatonID)
}
