// decision_records_comm.go — the decision record verbs over the chat
// channel, so the operator can read, create and withdraw decision records
// from Signal / Telegram without curl. The verbs proxy the REST surface
// (internal/server/decision_records.go) through the loopback helpers:
//
//	autonomous decision-records [automaton-id]
//		ten newest records, one per line: the id, the question state, the
//		title cut to 80 characters and, when answered, who answered and the
//		quote cut to 120 characters. A read.
//	autonomous decision-record <id>
//		the full record: title, situation, options with their trade-offs,
//		the recommendation, the state, the answer, the document change
//		report and the follow-up. A read.
//	autonomous decision-record create <json>
//		create a record from a JSON body (the same content fields as the
//		REST create). The reply names the record id, the question id and
//		any clarity warnings; a refusal replies with the findings.
//	autonomous decision-record withdraw <id> [reason]

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// decisionRecordsUsage is the usage line for the decision record comm
	// verbs; a bad verb or a missing argument replies with it.
	decisionRecordsUsage = "usage: autonomous decision-records [automaton-id] | decision-record <id> | decision-record create <json> | decision-record withdraw <id> [reason]"

	// decisionRecordsMaxLines is the most lines one list reply carries.
	decisionRecordsMaxLines = 10

	// decisionRecordsTitleCut and decisionRecordsQuoteCut are the rune
	// limits for the list line: the title and the answer quote.
	decisionRecordsTitleCut = 80
	decisionRecordsQuoteCut = 120
)

// decisionRecordLine is one record of the list reply.
type decisionRecordLine struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	State         string `json:"state"`
	QuestionState string `json:"question_state"`
	Answer        *struct {
		Actor string `json:"actor"`
		Quote string `json:"quote"`
	} `json:"answer"`
}

// decisionFinding is one clarity finding on the wire.
type decisionFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// decisionCreateWire is the create reply.
type decisionCreateWire struct {
	Record     decisionRecordLine `json:"record"`
	QuestionID string             `json:"question_id"`
	Warnings   []decisionFinding  `json:"warnings"`
}

// DecisionRecordsCommVerbs lists the decision record comm verbs for the
// parity test. Keep it in step with the verbs handleAutonomous routes to
// this handler.
func DecisionRecordsCommVerbs() []string {
	return []string{"decision-records", "decision-record"}
}

// handleDecisionRecordsVerb answers the autonomous command's decision record
// verbs.
func (r *Router) handleDecisionRecordsVerb(verb string, args []string) {
	switch verb {
	case "decision-records", "decision_records":
		r.decisionRecordsList(args)
	case "decision-record", "decision_record":
		r.decisionRecord(args)
	default:
		r.reply("autonomous decision-record failed", decisionRecordsUsage)
	}
}

// decisionRecordsList renders at most ten records, newest first, one per
// line.
func (r *Router) decisionRecordsList(args []string) {
	if len(args) > 2 {
		r.reply("autonomous decision-records failed", decisionRecordsUsage)
		return
	}
	q := url.Values{"limit": {strconv.Itoa(decisionRecordsMaxLines)}}
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		q.Set("prd_id", strings.TrimSpace(args[1]))
	}
	out, err := r.commGet("/api/autonomous/decision-records", q)
	if err != nil {
		r.reply("autonomous decision-records failed", err.Error())
		return
	}
	var list struct {
		DecisionRecords []decisionRecordLine `json:"decision_records"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		r.reply("autonomous decision-records failed", err.Error())
		return
	}
	if len(list.DecisionRecords) == 0 {
		r.reply("autonomous decision-records", "No decision records to show.")
		return
	}
	rows := list.DecisionRecords
	if len(rows) > decisionRecordsMaxLines {
		rows = rows[:decisionRecordsMaxLines]
	}
	var b strings.Builder
	for i, rec := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		state := rec.QuestionState
		if state == "" {
			state = rec.State
		}
		b.WriteString(rec.ID)
		b.WriteString(" ")
		b.WriteString(state)
		b.WriteString(" ")
		b.WriteString(cutToRunes(rec.Title, decisionRecordsTitleCut))
		if rec.Answer != nil && (rec.Answer.Actor != "" || rec.Answer.Quote != "") {
			b.WriteString(" | answered by ")
			b.WriteString(rec.Answer.Actor)
			if rec.Answer.Quote != "" {
				b.WriteString(": \"")
				b.WriteString(cutToRunes(rec.Answer.Quote, decisionRecordsQuoteCut))
				b.WriteString("\"")
			}
		}
	}
	r.reply("autonomous decision-records", b.String())
}

// decisionRecord answers the one-record verbs: show, create, withdraw.
func (r *Router) decisionRecord(args []string) {
	if len(args) < 2 {
		r.reply("autonomous decision-record failed", decisionRecordsUsage)
		return
	}
	switch strings.ToLower(args[1]) {
	case "create":
		r.decisionRecordCreate(args)
	case "withdraw":
		r.decisionRecordWithdraw(args)
	default:
		r.decisionRecordShow(args[1])
	}
}

// decisionRecordShow replies with the full record, pretty-printed.
func (r *Router) decisionRecordShow(id string) {
	if id = strings.TrimSpace(id); id == "" {
		r.reply("autonomous decision-record failed", decisionRecordsUsage)
		return
	}
	path := "/api/autonomous/decision-records/" + url.PathEscape(id)
	out, err := r.commGet(path, nil)
	if err != nil {
		r.reply("autonomous decision-record failed", err.Error())
		return
	}
	r.reply("autonomous decision-record", prettyJSON(out))
}

// decisionRecordCreate sends the JSON body to the create route. The reply
// names the record id, the question id and any clarity warnings; a refusal
// replies with the findings.
func (r *Router) decisionRecordCreate(args []string) {
	body := strings.TrimSpace(strings.Join(args[2:], " "))
	if body == "" {
		r.reply("autonomous decision-record create failed", decisionRecordsUsage)
		return
	}
	if !json.Valid([]byte(body)) {
		r.reply("autonomous decision-record create failed", "create takes one JSON object: "+decisionRecordsUsage)
		return
	}
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/decision-records", body)
	if err != nil {
		r.reply("autonomous decision-record create failed", decisionRecordErrorText(err))
		return
	}
	var wire decisionCreateWire
	if err := json.Unmarshal([]byte(out), &wire); err != nil {
		r.reply("autonomous decision-record create", prettyJSON(out))
		return
	}
	var b strings.Builder
	if wire.Record.ID != "" {
		b.WriteString("record " + wire.Record.ID)
	}
	if wire.QuestionID != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("question " + wire.QuestionID)
	}
	for _, w := range wire.Warnings {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("warning: ")
		if w.Code != "" {
			b.WriteString(w.Code + ": ")
		}
		b.WriteString(w.Message)
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		text = prettyJSON(out)
	}
	r.reply("autonomous decision-record create", text)
}

// decisionRecordWithdraw sends the optional reason and replies with the
// withdrawn record.
func (r *Router) decisionRecordWithdraw(args []string) {
	if len(args) < 3 {
		r.reply("autonomous decision-record withdraw failed", decisionRecordsUsage)
		return
	}
	id := strings.TrimSpace(args[2])
	if id == "" {
		r.reply("autonomous decision-record withdraw failed", decisionRecordsUsage)
		return
	}
	body := ""
	if reason := strings.TrimSpace(strings.Join(args[3:], " ")); reason != "" {
		raw, _ := json.Marshal(map[string]string{"reason": reason})
		body = string(raw)
	}
	path := "/api/autonomous/decision-records/" + url.PathEscape(id) + "/withdraw"
	out, err := r.commJSON(http.MethodPost, path, body)
	if err != nil {
		r.reply("autonomous decision-record withdraw failed", err.Error())
		return
	}
	r.reply("autonomous decision-record withdraw", prettyJSON(out))
}

// decisionRecordErrorText renders a failed call. The daemon answers a
// create refusal with {"error": "...", "findings": [...]}; render the error
// and every finding on its own line. Any other body is returned as sent.
func decisionRecordErrorText(err error) string {
	text := err.Error()
	// commDo reports "HTTP <status>: <body>".
	i := strings.Index(text, ": ")
	if i < 0 {
		return text
	}
	var wire struct {
		Error    string            `json:"error"`
		Findings []decisionFinding `json:"findings"`
	}
	if json.Unmarshal([]byte(text[i+len(": "):]), &wire) != nil {
		return text
	}
	if wire.Error == "" && len(wire.Findings) == 0 {
		return text
	}
	var b strings.Builder
	if wire.Error != "" {
		b.WriteString(wire.Error)
	}
	for _, f := range wire.Findings {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		if f.Code != "" {
			b.WriteString(f.Code + ": ")
		}
		b.WriteString(f.Message)
	}
	return b.String()
}

// cutToRunes trims s, cuts it to at most n runes and marks a cut with an
// ellipsis, so the result is never longer than n runes.
func cutToRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
