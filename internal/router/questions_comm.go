// questions_comm.go — the `question` chat verb: the operator question
// channel over comm (decision item 23). Mirrors the /api/questions REST
// surface (internal/server/questions.go):
//
//	question list                                — GET /api/questions?state=open
//	question show <id>                           — GET /api/questions/{id}
//	question answer <id> <N|option id> [quote…]  — resolve the option, POST /api/questions/{id}/answer
//
// The three subverbs always exist. The bare-reply rule
// (tryQuestionBareReply, called from the router's default branch) lets a
// plain message answer the open question when there is exactly one: the
// first word is an option number, or the message exactly matches an
// option label — the words after the number are the verbatim quote. With
// two or more open questions a bare reply is refused and the open
// questions are listed so `question answer <id>` can be used. With none
// open the message falls through to the existing plain-text handling.
// Every loopback uses the same commGet/commJSON helpers as the other
// comm-parity commands, and every answer records surface
// "comm:<channel>" and the sender's comm identity.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// questionsListPath is the open-questions collection read for `question
// list` and for the bare-reply rule.
const questionsListPath = "/api/questions"

// questionUsage is the text sent when a subverb is missing or malformed.
const questionUsage = "usage: question list | question show <id> | question answer <id> <option number or id> [quote...]"

func questionPath(id, sub string) string {
	p := questionsListPath + "/" + url.PathEscape(strings.TrimSpace(id))
	if sub != "" {
		p += "/" + sub
	}
	return p
}

// questionWire mirrors the question record the daemon returns (a subset
// of autonomous.Question, the fields these verbs render).
type questionWire struct {
	ID             string               `json:"id"`
	Label          string               `json:"label"`
	Text           string               `json:"text"`
	State          string               `json:"state"`
	Options        []questionOptionWire `json:"options"`
	Recommendation *struct {
		OptionID  string `json:"option_id"`
		Rationale string `json:"rationale"`
	} `json:"recommendation"`
}

type questionOptionWire struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// questionListBody is the GET /api/questions?state=open response.
type questionListBody struct {
	Count     int            `json:"count"`
	Questions []questionWire `json:"questions"`
}

// questionAnswerWire is the current-answer summary on a question read.
type questionAnswerWire struct {
	ID             string `json:"id"`
	ChosenOptionID string `json:"chosen_option_id"`
	Quote          string `json:"quote"`
	Actor          string `json:"actor"`
	Surface        string `json:"surface"`
}

// questionGetBody is the GET /api/questions/{id} response.
type questionGetBody struct {
	Question        questionWire        `json:"question"`
	AnswerCount     int                 `json:"answer_count"`
	CurrentAnswerID string              `json:"current_answer_id"`
	Answer          *questionAnswerWire `json:"answer"`
}

// questionVerb is the parsed `question` subverb.
type questionVerb struct {
	action string // list | show | answer
	id     string
	option string // raw option token (a number or an option id)
	quote  string // remainder after the option token, as typed
}

// parseQuestionVerb splits the text after "question" into subverb +
// args. The quote for `answer` is every word after the option token,
// kept as typed: internal spaces and pasted newlines survive; only the
// one field separator after the token is not part of the quote.
func parseQuestionVerb(text string) (questionVerb, error) {
	rest := strings.TrimSpace(text)
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return questionVerb{}, fmt.Errorf("%s", questionUsage)
	}
	switch parts[0] {
	case "list":
		if len(parts) != 1 {
			return questionVerb{}, fmt.Errorf("question list takes no arguments (%s)", questionUsage)
		}
		return questionVerb{action: "list"}, nil
	case "show":
		if len(parts) != 2 {
			return questionVerb{}, fmt.Errorf("question show takes one id (%s)", questionUsage)
		}
		return questionVerb{action: "show", id: parts[1]}, nil
	case "answer":
		if len(parts) < 3 {
			return questionVerb{}, fmt.Errorf("question answer takes <id> and <option number or id> (%s)", questionUsage)
		}
		// The quote is every word after the option token, kept verbatim
		// (newline- and space-preserving): operators may paste a longer
		// answer than a single line.
		return questionVerb{action: "answer", id: parts[1], option: parts[2],
			quote: questionQuote(rest, 3)}, nil
	}
	return questionVerb{}, fmt.Errorf("%s", questionUsage)
}

// questionQuote returns the remainder of text after its first n
// whitespace-delimited fields. The one separator between the last field
// and the quote is not part of the quote; every other byte is:
// internal and trailing spaces, newlines, an intentionally pasted blank
// line — kept exactly as typed.
func questionQuote(text string, n int) string {
	t := strings.TrimSpace(text)
	for i := 0; i < n; i++ {
		t = strings.TrimLeft(t, " \t\n\r")
		j := strings.IndexAny(t, " \t\n\r")
		if j < 0 {
			return "" // field i is the last token: no remainder
		}
		if i == n-1 {
			return strings.TrimLeft(t[j:], " \t\n\r") // the quote
		}
		t = t[j:]
	}
	return t
}

// handleQuestion implements the `question` chat verb: it parses the
// subverb and performs the matching loopback.
func (r *Router) handleQuestion(cmd Command) {
	v, err := parseQuestionVerb(cmd.Text)
	if err != nil {
		r.reply("question", err.Error())
		return
	}
	switch v.action {
	case "list":
		r.handleQuestionList()
	case "show":
		r.handleQuestionShow(v.id)
	case "answer":
		r.handleQuestionAnswer(v.id, v.option, v.quote)
	}
}

// handleQuestionList GETs the open questions and sends them as a
// numbered list with id, label and text.
func (r *Router) handleQuestionList() {
	body, err := r.commGet(questionsListPath, url.Values{"state": {"open"}})
	if err != nil {
		r.reply("question list failed", err.Error())
		return
	}
	var vb questionListBody
	if err := json.Unmarshal([]byte(body), &vb); err != nil {
		r.reply("question list failed", "cannot read daemon response: "+err.Error())
		return
	}
	if len(vb.Questions) == 0 {
		r.reply("question list", "no open questions")
		return
	}
	var b strings.Builder
	b.WriteString("open questions:")
	for i, q := range vb.Questions {
		fmt.Fprintf(&b, "\n%d. %s  %s", i+1, q.ID, q.Label)
		if q.Text != "" {
			b.WriteString("\n   " + q.Text)
		}
	}
	r.reply("question list", b.String())
}

// handleQuestionShow GETs one question and renders it with its numbered
// options, the recommendation (if any) and the current answer's quote.
func (r *Router) handleQuestionShow(id string) {
	body, err := r.commGet(questionPath(id, ""), nil)
	if err != nil {
		r.reply("question show failed", err.Error())
		return
	}
	var vb questionGetBody
	if err := json.Unmarshal([]byte(body), &vb); err != nil {
		r.reply("question show failed", "cannot read daemon response: "+err.Error())
		return
	}
	q := &vb.Question
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s", q.ID, q.Label)
	if q.State != "" {
		fmt.Fprintf(&b, "  (%s)", q.State)
	}
	b.WriteString("\n" + q.Text)
	if len(q.Options) > 0 {
		b.WriteString("\noptions:")
		byID := map[string]questionOptionWire{}
		for i, o := range q.Options {
			byID[o.ID] = o
			fmt.Fprintf(&b, "\n%d. %s  %s", i+1, o.ID, o.Label)
		}
		if q.Recommendation != nil && q.Recommendation.OptionID != "" {
			label := q.Recommendation.OptionID
			if o, ok := byID[q.Recommendation.OptionID]; ok {
				label = o.Label
			}
			fmt.Fprintf(&b, "\nrecommended: %s", label)
			if q.Recommendation.Rationale != "" {
				b.WriteString(" — " + q.Recommendation.Rationale)
			}
		}
	}
	if vb.Answer != nil {
		chosen := vb.Answer.ChosenOptionID
		if byID := labelForQuestionOption(q, chosen); byID != "" {
			chosen = byID
		}
		fmt.Fprintf(&b, "\nanswer (option %s):", chosen)
		if vb.Answer.Quote != "" {
			b.WriteString("\n" + vb.Answer.Quote)
		}
	}
	r.reply("question show", b.String())
}

// handleQuestionAnswer resolves the option token (a 1-based number or
// an option id) against the question, then POSTs the answer with the
// verbatim quote, the comm surface and the sender identity.
func (r *Router) handleQuestionAnswer(id, optionToken, quote string) {
	body, err := r.commGet(questionPath(id, ""), nil)
	if err != nil {
		r.reply("question answer failed", err.Error())
		return
	}
	var vb questionGetBody
	if err := json.Unmarshal([]byte(body), &vb); err != nil {
		r.reply("question answer failed", "cannot read daemon response: "+err.Error())
		return
	}
	optID, ok := resolveQuestionOption(&vb.Question, optionToken)
	if !ok {
		r.reply("question answer failed",
			fmt.Sprintf("unknown option %q — valid options for %s: %s",
				optionToken, id, questionOptionList(&vb.Question)))
		return
	}
	r.recordQuestionAnswer(id, optID, quote)
}

// recordQuestionAnswer performs the POST /api/questions/{id}/answer
// loopback and reports the result. Shared by the explicit subverb and
// the bare-reply rule.
func (r *Router) recordQuestionAnswer(id, optionID, quote string) {
	b, err := json.Marshal(map[string]string{
		"chosen_option_id": optionID,
		"quote":            quote,
		"surface":          "comm:" + r.commChannelName(),
		"actor":            r.commSubIdentity(),
	})
	if err != nil {
		r.reply("question answer failed", err.Error())
		return
	}
	body, err := r.commJSON("POST", questionPath(id, "answer"), string(b))
	if err != nil {
		r.reply("question answer failed", err.Error())
		return
	}
	r.reply("question answered", body)
}

// resolveQuestionOption maps an option token (a 1-based option number or
// an exact option id) onto the question's option id set.
func resolveQuestionOption(q *questionWire, token string) (string, bool) {
	trimmed := strings.TrimSpace(token)
	if n, err := strconv.Atoi(trimmed); err == nil && n >= 1 && n <= len(q.Options) {
		return q.Options[n-1].ID, true
	}
	for _, o := range q.Options {
		if o.ID == trimmed {
			return o.ID, true
		}
	}
	return "", false
}

// questionOptionList renders the valid options as "1: <id> <label>; ..."
// for a usage refusal.
func questionOptionList(q *questionWire) string {
	var b strings.Builder
	for i, o := range q.Options {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%d: %s %s", i+1, o.ID, o.Label)
	}
	return strings.TrimSpace(b.String())
}

func labelForQuestionOption(q *questionWire, optionID string) string {
	for _, o := range q.Options {
		if o.ID == optionID {
			return o.Label
		}
	}
	return ""
}

// ── bare-reply rule ─────────────────────────────────────────────────────

// tryQuestionBareReply implements the bare-reply rule for the default
// plain-text branch. It reads the open questions; if exactly one is open
// and the message looks like an answer to it (first word = option
// number, or the message exactly matches an option label), it records
// the answer. With several open questions a matching message is refused
// with the list of open questions and a pointer to `question answer`.
// It returns true when the message was consumed as a bare reply
// (answered or refused); false means the caller must run the existing
// plain-text handling. A daemon request failure — down, unconfigured, or
// an unreadable response — also returns false (fall through), never a
// refusal.
func (r *Router) tryQuestionBareReply(text string) bool {
	if r.webPort == 0 {
		return false
	}
	trimmed := strings.TrimSpace(text)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	first := fields[0]

	open, err := r.commOpenQuestions()
	if err != nil {
		return false
	}
	if len(open) == 0 {
		// No open question: the message is not a bare reply.
		return false
	}

	// Is this message shaped like an answer to one of the open
	// questions? (First word = an option number of it, or the whole
	// message = an exact option label of it.)
	matched := 0
	for i := range open {
		if questionMessageMatchesOpen(&open[i], first, trimmed) {
			matched++
		}
	}
	if matched == 0 {
		return false
	}
	if len(open) > 1 {
		// Several open questions: a bare reply cannot say which one it
		// means — refuse and hand the list back.
		var b strings.Builder
		b.WriteString("several questions are open — use `question answer <id> <option number or id>` to answer one:")
		for i, q := range open {
			fmt.Fprintf(&b, "\n%d. %s  %s", i+1, q.ID, q.Label)
		}
		r.reply("question answer refused", b.String())
		return true
	}

	q := &open[0]
	// First word is an option number: the words after it are the quote.
	if n, err := strconv.Atoi(first); err == nil && n >= 1 && n <= len(q.Options) {
		r.recordQuestionAnswer(q.ID, q.Options[n-1].ID, bareReplyQuote(trimmed, first))
		return true
	}
	// The message exactly matches an option label.
	for _, o := range q.Options {
		if trimmed == o.Label {
			r.recordQuestionAnswer(q.ID, o.ID, "")
			return true
		}
	}
	return false
}

// commOpenQuestions GETs the open questions and decodes them.
func (r *Router) commOpenQuestions() ([]questionWire, error) {
	body, err := r.commGet(questionsListPath, url.Values{"state": {"open"}})
	if err != nil {
		return nil, err
	}
	var vb questionListBody
	if err := json.Unmarshal([]byte(body), &vb); err != nil {
		return nil, err
	}
	return vb.Questions, nil
}

// questionMessageMatchesOpen reports whether a plain message looks like
// an answer to q: its first word is one of q's option numbers, or the
// whole message exactly matches one of q's option labels.
func questionMessageMatchesOpen(q *questionWire, first, whole string) bool {
	for i := range q.Options {
		if strconv.Itoa(i+1) == first || whole == q.Options[i].Label {
			return true
		}
	}
	return false
}

// bareReplyQuote returns the words of a bare reply after its leading
// option number: internal newlines and spaces are kept exactly as
// typed; only the one separator right after the number drops.
func bareReplyQuote(text, numberToken string) string {
	rest := text
	for {
		idx := strings.Index(rest, numberToken)
		if idx < 0 {
			return ""
		}
		after := rest[idx+len(numberToken):]
		if before := rest[:idx]; strings.TrimSpace(before) == "" {
			return strings.TrimLeft(after, " \t\n\r")
		}
		rest = after
	}
}
