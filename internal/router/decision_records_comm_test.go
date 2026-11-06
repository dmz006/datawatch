// decision_records_comm_test.go — the decision record verbs over the comm
// channel: the list and the full record, create (valid and refused),
// withdraw, the usage line, and the verbs list against the handler.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// drCommListBody is n records, newest first, mixed answered / open.
func drCommListBody(n int) string {
	parts := make([]string, 0, n)
	for i := n; i >= 1; i-- {
		state := "open"
		extra := ""
		if i%3 == 0 {
			state = "answered"
			extra = `,"question_state":"answered","answer":{"actor":"dmz","quote":"go with the cheaper option"}`
		}
		parts = append(parts, fmt.Sprintf(
			`{"id":"dr-%02d","title":"%s","state":"%s"%s}`,
			i, fmt.Sprintf("Title number %02d", i), state, extra))
	}
	return `{"decision_records":[` + strings.Join(parts, ",") + `],"folder":{}}`
}

func TestDecisionRecordComm_ListReplyShapeAndTenCap(t *testing.T) {
	// 97 runes: over the 80-rune cut, so the reply must carry the
	// ellipsis and never the whole title.
	longTitle := "Should the daemon keep every operator decision record in one sqlite table at all?"
	body := `{"decision_records":[
		{"id":"dr-b1","title":"` + longTitle + `","state":"answered",
		 "question_state":"answered","answer":{"actor":"dmz","quote":"sqlite, please"}},
		{"id":"dr-b0","title":"Ship on Friday?","state":"open","question_state":"open"}
	],"folder":{}}`
	log, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous decision-records p1"), "\n")
	if log.path != "/api/autonomous/decision-records" || !strings.Contains(log.query, "prd_id=p1") || !strings.Contains(log.query, "limit=10") {
		t.Errorf("path=%s query=%q, want the decision records route with prd_id and limit=10", log.path, log.query)
	}
	for _, want := range []string{
		"dr-b1 answered Should the daemon keep every operat",
		`| answered by dmz: "sqlite, please"`,
		"dr-b0 open Ship on Friday?",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	if i1, i0 := strings.Index(got, "dr-b1"), strings.Index(got, "dr-b0"); i1 < 0 || i0 < 0 || i1 >= i0 {
		t.Errorf("newest records must come first:\n%s", got)
	}
	// The title is cut at 80 runes with an ellipsis.
	if !strings.Contains(got, "\u2026") || strings.Contains(got, longTitle) {
		t.Errorf("the long title must be cut with an ellipsis:\n%s", got)
	}

	// An empty list prints its own line.
	_, port = laneFillCommDaemon(t, http.StatusOK, `{"decision_records":[],"folder":{}}`)
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous decision-records"), " "); !strings.Contains(got, "No decision records to show.") {
		t.Errorf("empty list reply = %q", got)
	}

	// More than ten records: only the ten newest lines are sent.
	_, port = laneFillCommDaemon(t, http.StatusOK, drCommListBody(14))
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous decision-records"), "\n")
	for _, want := range []string{"dr-14", "dr-05"} {
		if !strings.Contains(got, want) {
			t.Errorf("the ten newest must include %s:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"dr-04", "dr-01"} {
		if strings.Contains(got, notWant) {
			t.Errorf("the reply must hold ten lines at most but shows %s:\n%s", notWant, got)
		}
	}
}

func TestDecisionRecordComm_ShowFullRecord(t *testing.T) {
	body := `{"record":{
		 "id":"dr-zx1","title":"Where does the decision record live?","situation":"The daemon needs a durable record of operator decisions.",
		 "options":[{"id":"opt-a","label":"a table in sqlite","tradeoff":"portable, no new store"},{"id":"opt-b","label":"a JSON file","tradeoff":"easy to read, no concurrency"}],
		 "recommendation":"opt-a","state":"answered","question_state":"answered",
		 "answer":{"answer_id":"drans-9","chosen_option_id":"opt-a","actor":"dmz","surface":"chat","quote":"a table in sqlite"},
		 "doc_change":"docs/decisions.md gained a row.","follow_up":"none"
		},"question_id":"q-zx1"}`
	log, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous decision-record dr-zx1"), " ")
	if log.path != "/api/autonomous/decision-records/dr-zx1" {
		t.Errorf("path=%s, want the decision record route", log.path)
	}
	for _, want := range []string{
		"Where does the decision record live?",
		"The daemon needs a durable record of operator decisions.",
		"a table in sqlite",
		"easy to read, no concurrency",
		"docs/decisions.md gained a row.",
		"q-zx1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestDecisionRecordComm_CreateSendsBodyAndNamesIdsAndWarnings(t *testing.T) {
	out := `{"record":{"id":"dr-n1","title":"Use sqlite for decision records?","state":"open"},
		 "question_id":"q-n1",
		 "warnings":[{"code":"option_tradeoff","message":"option b has no trade-off"}]}`
	log, port := laneFillCommDaemon(t, http.StatusCreated, out)
	r := setupPeerRouter(t, port)
	req := `{"title":"Use sqlite for decision records?","situation":"records need a store","options":[{"id":"a","label":"a sqlite table"},{"id":"b","label":"a json file"}]}`
	got := strings.Join(r.HandleTestMessage("autonomous decision-record create "+req), " ")
	if log.path != "/api/autonomous/decision-records" {
		t.Errorf("path=%s, want the create route", log.path)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(log.body), &sent); err != nil {
		t.Fatalf("the body the router sent is not an object: %q", log.body)
	}
	if sent["title"] != "Use sqlite for decision records?" || len(sent) < 3 {
		t.Errorf("the router must pass the JSON body through, got %s", log.body)
	}
	for _, want := range []string{"record dr-n1", "question q-n1", "warning: option_tradeoff: option b has no trade-off"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestDecisionRecordComm_CreateRefusalRepliesFindings(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusUnprocessableEntity,
		`{"error":"decision record refused: it is not clear enough; fix the findings and send it again",
		  "findings":[{"code":"clarity_question","message":"the situation is empty"},
	                      {"code":"clarity_option","message":"both options say the same thing"}]}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage(`autonomous decision-record create {"title":"t"}`), "\n")
	if !strings.Contains(got, "decision-record create failed") {
		t.Errorf("a refusal must be a failure reply, got %q", got)
	}
	for _, want := range []string{
		"it is not clear enough; fix the findings and send it again",
		"clarity_question: the situation is empty",
		"clarity_option: both options say the same thing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestDecisionRecordComm_WithdrawPostsReasonAndConfirms(t *testing.T) {
	out := `{"record":{"id":"dr-w1","title":"Ship on Friday?","state":"withdrawn"},"question_id":"q-w1"}`
	log, port := laneFillCommDaemon(t, http.StatusOK, out)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous decision-record withdraw dr-w1 premature"), " ")
	if log.path != "/api/autonomous/decision-records/dr-w1/withdraw" {
		t.Errorf("path=%s, want the withdraw route", log.path)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(log.body), &sent); err != nil || sent["reason"] != "premature" || len(sent) != 1 {
		t.Errorf("body=%q, want {\"reason\":\"premature\"}", log.body)
	}
	for _, want := range []string{"autonomous decision-record withdraw", "dr-w1", "withdrawn"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}

	// No reason: the route is still hit, with an empty body.
	log, port = laneFillCommDaemon(t, http.StatusOK, `{"record":{"id":"dr-w2","state":"withdrawn"},"question_id":"q-w2"}`)
	r = setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous decision-record withdraw dr-w2")
	if log.path != "/api/autonomous/decision-records/dr-w2/withdraw" || log.body != "" {
		t.Errorf("path=%s body=%q, want the withdraw route with an empty body", log.path, log.body)
	}
}

func TestDecisionRecordComm_BadVerbAndMissingArgsShowUsageAndSendNothing(t *testing.T) {
	for _, msg := range []string{
		"autonomous decision-record",
		"autonomous decision-record withdraw",
		"autonomous decision-record create",
		"autonomous decision-record create not json",
		"autonomous decision-records a b c",
	} {
		log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage: autonomous decision-records") {
			t.Errorf("%q: expected the usage, got %q", msg, got)
		}
		if log.calls != 0 {
			t.Errorf("%q: a local refusal must not reach the daemon, got %d calls", msg, log.calls)
		}
	}

	// The autonomous help lists every verb and its shape.
	r := &Router{}
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	for _, want := range []string{
		"decision-records [automaton-id]",
		"decision-record <id>",
		"decision-record create <json>",
		"decision-record withdraw <id> [reason]",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("autonomous help lacks %q:\n%s", want, help)
		}
	}
}

func TestDecisionRecordComm_VerbsListMatchesHandler(t *testing.T) {
	verbs := DecisionRecordsCommVerbs()
	for _, want := range []string{"decision-records", "decision-record"} {
		if !slices.Contains(verbs, want) {
			t.Errorf("DecisionRecordsCommVerbs() = %v, must contain %q", verbs, want)
		}
	}
	for _, verb := range verbs {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}
	// Every verb in the list reaches this handler, not the unknown-verb help:
	// with no daemon running the handler answers with the usage (no-record
	// form) or the loopback error, and never "unknown verb".
	r := &Router{}
	got := strings.Join(r.HandleTestMessage("autonomous decision-record"), " ")
	if !strings.Contains(got, "usage: autonomous decision-records") || strings.Contains(got, "unknown verb") {
		t.Errorf("decision-record was not routed to its handler: %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous decision-record dr-x1"), " ")
	if strings.Contains(got, "unknown verb") || !strings.Contains(got, "decision-record failed") {
		t.Errorf("decision-record <id> was not routed to its handler: %q", got)
	}
}
