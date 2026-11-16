// Quick replies on a messaging channel answer a session's open question
// instead of typing into the session when the reply means an answer —
// the same routing the web app uses for its quick reply buttons.

package router

import (
	"fmt"
	"strings"
)

// quickReplySynonymGroups is the small approve/reject synonym map, kept in
// step with the web app: a reply in a group matches an option whose id or
// label is in the same group.
var quickReplySynonymGroups = [][]string{
	{"yes", "approve", "approved"},
	{"no", "reject", "rejected"},
}

func quickReplySynonymGroup(word string) []string {
	for _, g := range quickReplySynonymGroups {
		for _, w := range g {
			if w == word {
				return g
			}
		}
	}
	return nil
}

func inWordSet(set []string, w string) bool {
	for _, s := range set {
		if s == w {
			return true
		}
	}
	return false
}

// quickReplyQuestionForSession reports whether a question belongs to the
// session; session_id may carry the short or the full id.
func quickReplyQuestionForSession(q *questionWire, shortID, fullID string) bool {
	qid := strings.TrimSpace(q.SessionID)
	if qid == "" {
		return false
	}
	last := func(s string) string {
		if i := strings.LastIndex(s, "-"); i >= 0 {
			return s[i+1:]
		}
		return s
	}
	return qid == shortID || qid == fullID || last(qid) == last(fullID) || last(qid) == shortID
}

// quickReplyOptionFor resolves the reply candidates (lower-cased) against
// the options. It returns the option id, or "" when nothing matches or
// several options match.
func quickReplyOptionFor(opts []questionOptionWire, cands []string) string {
	if len(cands) == 0 || len(opts) == 0 {
		return ""
	}
	var direct []string
	for _, o := range opts {
		if inWordSet(cands, strings.ToLower(o.ID)) || inWordSet(cands, strings.ToLower(o.Label)) {
			direct = append(direct, o.ID)
		}
	}
	if len(direct) == 1 {
		return direct[0]
	}
	if len(direct) > 1 {
		return ""
	}
	for _, c := range cands {
		g := quickReplySynonymGroup(c)
		if g == nil {
			continue
		}
		var m []string
		for _, o := range opts {
			if inWordSet(g, strings.ToLower(o.ID)) || inWordSet(g, strings.ToLower(o.Label)) {
				m = append(m, o.ID)
			}
		}
		if len(m) == 1 {
			return m[0]
		}
		if len(m) > 1 {
			return ""
		}
	}
	return ""
}

// quickReplyCandidates returns the lower-cased words a reply may match:
// the saved command name (for !name or /name) and the expanded text.
func quickReplyCandidates(raw, expanded string) []string {
	var out []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !inWordSet(out, s) {
			out = append(out, s)
		}
	}
	t := strings.TrimSpace(raw)
	if len(t) >= 2 && (t[0] == '!' || t[0] == '/') {
		add(t[1:])
	} else {
		add(t)
	}
	add(expanded)
	return out
}

// sessionOpenQuestions returns the open questions of one session. An
// unreachable or unconfigured daemon yields an error.
func (r *Router) sessionOpenQuestions(shortID, fullID string) ([]questionWire, error) {
	if r.webPort == 0 {
		return nil, fmt.Errorf("daemon not configured")
	}
	all, err := r.commOpenQuestions()
	if err != nil {
		return nil, err
	}
	var out []questionWire
	for i := range all {
		if quickReplyQuestionForSession(&all[i], shortID, fullID) {
			out = append(out, all[i])
		}
	}
	return out, nil
}

func questionOptionLabels(q *questionWire) string {
	labels := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		l := o.Label
		if l == "" {
			l = o.ID
		}
		labels = append(labels, l)
	}
	return strings.Join(labels, ", ")
}

// tryQuickReplyQuestion routes a send-to-session reply to the session's
// open question when the reply is a saved command quick reply or a plain
// answer word. It returns true when the reply was consumed (answered or
// refused); false means type the text into the session as before.
func (r *Router) tryQuickReplyQuestion(shortID, fullID, raw, expanded string) bool {
	t := strings.TrimSpace(raw)
	if t == "" {
		return false
	}
	isQuick := len(t) >= 2 && (t[0] == '!' || t[0] == '/')
	cands := quickReplyCandidates(raw, expanded)

	open, err := r.sessionOpenQuestions(shortID, fullID)
	if err != nil || len(open) == 0 {
		return false
	}

	answerWord := isQuick
	for _, c := range cands {
		if quickReplySynonymGroup(c) != nil {
			answerWord = true
		}
		for i := range open {
			for _, o := range open[i].Options {
				if c == strings.ToLower(o.ID) || c == strings.ToLower(o.Label) {
					answerWord = true
				}
			}
		}
	}
	if !answerWord {
		return false
	}

	if len(open) > 1 {
		var b strings.Builder
		fmt.Fprintf(&b, "session %s has %d open questions — nothing was sent. Use `question answer <id> <option>` to answer one:", shortID, len(open))
		for i, q := range open {
			fmt.Fprintf(&b, "\n%d. %s  %s", i+1, q.ID, q.Label)
		}
		r.reply("quick reply refused", b.String())
		return true
	}
	q := &open[0]
	optID := quickReplyOptionFor(q.Options, cands)
	if optID == "" {
		r.reply("quick reply refused", fmt.Sprintf(
			"%q does not match one option of the open question %s — nothing was sent. Options: %s (or use `question answer %s <option>`)",
			t, q.ID, questionOptionLabels(q), q.ID))
		return true
	}
	if _, err := r.postQuestionAnswer(q.ID, optID, ""); err != nil {
		r.reply("question answer failed", err.Error())
		return true
	}
	label := labelForQuestionOption(q, optID)
	if label == "" {
		label = optID
	}
	r.reply("question answered", fmt.Sprintf(
		"session %s question %s answered with option %s (%s); sent: %q",
		shortID, q.ID, label, optID, t))
	return true
}

// quickReplyAnswerHint names the answer words for the alert quick reply
// hint when the session has exactly one open question; "" otherwise.
func (r *Router) quickReplyAnswerHint(shortID, fullID string) string {
	open, err := r.sessionOpenQuestions(shortID, fullID)
	if err != nil || len(open) != 1 {
		return ""
	}
	labels := make([]string, 0, len(open[0].Options))
	for _, o := range open[0].Options {
		l := o.Label
		if l == "" {
			l = o.ID
		}
		labels = append(labels, l)
	}
	return strings.Join(labels, " | ")
}
