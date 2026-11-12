// Decision gate rule: a task spec that names an operator decision id keeps
// working only once that decision is answered. This file holds the pure
// rule (find the ids a spec names, check each against the question store);
// it has no executor wiring and no I/O beyond the store lookup. The
// config keys are autonomous.questions.decision_gate and
// autonomous.questions.decision_gate_mode.
package autonomous

import (
	"fmt"
	"regexp"
	"strings"
)

// decisionRefRe matches a decision id: q- followed by at least 20
// lowercase hex characters. The \b on each side makes the match a whole
// token: the id must not sit inside a longer word (a leading letter or
// digit before q) and must not run on into further word characters
// (a trailing non-hex letter or digit), so "xq-<hex>" and "q-<hex>z" do
// not match. Hex runs longer than 20 are kept whole.
var decisionRefRe = regexp.MustCompile(`\bq-[0-9a-f]{20,}\b`)

// decisionHoldTextMax is the number of characters of the question text
// quoted in the reason for a decision that is still open.
const decisionHoldTextMax = 160

// decisionStateMissing is the state reported for an id the store does not
// hold (or when there is no store).
const decisionStateMissing = "missing"

// DecisionHoldReason is one referenced decision that does not pass the gate.
type DecisionHoldReason struct {
	// ID is the decision id the spec names.
	ID string
	// State is open, withdrawn, expired, or missing for an unknown id.
	State string
	// Reason is the plain text shown to the operator.
	Reason string
}

// DecisionRefs returns the distinct decision ids the spec text names, in
// the order they first appear.
func DecisionRefs(spec string) []string {
	found := decisionRefRe.FindAllString(spec, -1)
	if len(found) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(found))
	out := make([]string, 0, len(found))
	for _, id := range found {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// DecisionHold returns one hold per decision id the spec names that does
// not pass. An answered decision passes. A nil store holds every
// referenced id as missing. A spec that names no id has no holds.
func DecisionHold(store *QuestionStore, spec string) []DecisionHoldReason {
	var holds []DecisionHoldReason
	for _, id := range DecisionRefs(spec) {
		if store == nil {
			holds = append(holds, missingDecisionHold(id))
			continue
		}
		q, ok := store.Get(id)
		if !ok {
			holds = append(holds, missingDecisionHold(id))
			continue
		}
		switch q.State {
		case QuestionAnswered:
			// passes
		case QuestionOpen:
			holds = append(holds, DecisionHoldReason{
				ID:     id,
				State:  string(QuestionOpen),
				Reason: fmt.Sprintf("operator decision %s is waiting for an answer: %s", id, truncateDecisionText(q.Text)),
			})
		default:
			holds = append(holds, DecisionHoldReason{
				ID:     id,
				State:  string(q.State),
				Reason: fmt.Sprintf("operator decision %s was %s without an answer", id, q.State),
			})
		}
	}
	return holds
}

func missingDecisionHold(id string) DecisionHoldReason {
	return DecisionHoldReason{
		ID:     id,
		State:  decisionStateMissing,
		Reason: fmt.Sprintf("operator decision %s was not found", id),
	}
}

// truncateDecisionText cuts text to decisionHoldTextMax characters (runes,
// so a multi-byte character is never split).
func truncateDecisionText(text string) string {
	text = strings.TrimSpace(text)
	r := []rune(text)
	if len(r) <= decisionHoldTextMax {
		return text
	}
	return string(r[:decisionHoldTextMax])
}
