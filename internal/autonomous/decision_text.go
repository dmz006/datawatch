// Decision line text: the CLI and the comm channel both render the body of
// GET /api/autonomous/decisions through this file, so no surface shows a
// different wording.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DecisionTextQuestionMax is how many characters of the question one line shows.
const DecisionTextQuestionMax = 120

// ParseDecisionLine reads the body of GET /api/autonomous/decisions.
func ParseDecisionLine(body []byte) ([]DecisionLineEntry, error) {
	var b struct {
		Decisions []DecisionLineEntry `json:"decisions"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("reading the decisions: %w", err)
	}
	if b.Decisions == nil {
		b.Decisions = []DecisionLineEntry{}
	}
	return b.Decisions, nil
}

// RenderDecisionEntry is one decision on one line: id, state and the
// question cut to 120 characters. An answered decision adds the answer id,
// the actor and the quote verbatim; a task held on the decision is named with
// its wait reason.
func RenderDecisionEntry(e DecisionLineEntry) string {
	q := strings.Join(strings.Fields(e.Question), " ")
	if r := []rune(q); len(r) > DecisionTextQuestionMax {
		q = string(r[:DecisionTextQuestionMax])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s", e.ID, e.State, q)
	if e.Answer != nil {
		fmt.Fprintf(&b, " | answer %s by %s: \"%s\"", e.Answer.ID, e.Answer.Actor, e.Answer.Quote)
	}
	waits := e.WaitingTasks
	if len(waits) == 0 {
		for _, id := range e.WaitingTaskIDs {
			waits = append(waits, DecisionLineWait{TaskID: id})
		}
	}
	if len(waits) > 0 {
		parts := make([]string, len(waits))
		for i, w := range waits {
			parts[i] = "task " + w.TaskID
			if w.WaitReason != "" {
				parts[i] += " (" + w.WaitReason + ")"
			}
		}
		b.WriteString(" | waiting: " + strings.Join(parts, "; "))
	}
	return b.String()
}

// RenderDecisionLines renders the decisions in the order the daemon sent
// them (newest first), one line each, with a trailing newline. max > 0 keeps
// only that many lines. An empty list says so.
func RenderDecisionLines(es []DecisionLineEntry, max int) string {
	if len(es) == 0 {
		return "No decisions to show.\n"
	}
	if max > 0 && len(es) > max {
		es = es[:max]
	}
	var b strings.Builder
	for _, e := range es {
		b.WriteString(RenderDecisionEntry(e))
		b.WriteString("\n")
	}
	return b.String()
}
