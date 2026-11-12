// question_notify.go — the messaging-group leg of question-raise
// notification. main.go wires the router into the server's
// QuestionRaiseNotifier seam; this method is the per-router delivery and
// goes through the same async send path HandleNeedsInput uses, so a slow
// or failing backend can never block the raise (which is already
// persisted and answered by the time we get here).
//
// The text names the question id and lists the options as a numbered
// list, and points the operator at the three answer routes: the
// question answer chat verb (with a bare option number when this is the
// only open question), the REST endpoint, and the Automaton PWA.

package router

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// NotifyQuestionRaised sends one raised question to this router's messaging
// group.
func (r *Router) NotifyQuestionRaised(q autonomous.Question) {
	r.send(questionRaiseText(r.hostname, q))
}

// questionRaiseText builds the group text: [hostname] marker, the question
// id and label, a high-priority marker when set, the question text, the
// options as a numbered list, and the three answer routes (the question
// answer chat verb, the REST endpoint, and the Automaton PWA).
func questionRaiseText(hostname string, q autonomous.Question) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] question %s", hostname, q.ID)
	if q.Label != "" {
		fmt.Fprintf(&b, " — %s", q.Label)
	}
	if q.Priority == autonomous.QuestionPriorityHigh {
		b.WriteString(" (high priority)")
	}
	b.WriteByte('\n')
	if q.Text != "" {
		b.WriteString(q.Text)
		b.WriteByte('\n')
	}
	if len(q.Options) > 0 {
		b.WriteString("Options:\n")
		for i, o := range q.Options {
			label := o.Label
			if label == "" {
				label = o.ID
			}
			fmt.Fprintf(&b, "%d. %s\n", i+1, label)
		}
	}
	b.WriteString("Answer: question answer ")
	b.WriteString(q.ID)
	b.WriteString(" <option number> followed by your words (a bare option number when this is the only open question), or POST /api/questions/")
	b.WriteString(q.ID)
	b.WriteString("/answer, or in the Automaton PWA.")
	return b.String()
}
