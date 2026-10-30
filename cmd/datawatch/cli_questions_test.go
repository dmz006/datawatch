// CLI tests for `datawatch question` (list, show, answer, withdraw, raise),
// against an httptest fake daemon (same pattern as cli_autonomous_events_test.go).

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// testQuestionJSON is one fully-shaped question record.
const testQuestionJSON = `{
  "id": "q_1",
  "label": "decision",
  "asked_by": "sup-1",
  "asker_kind": "supervisor",
  "context": "prd",
  "prd_id": "prd-1",
  "text": "Which recovery path should the Automaton take?",
  "options": [
    {"id": "o1", "label": "Retry now", "tradeoffs": "fast, but it may double the work"},
    {"id": "o2", "label": "Escalate to an operator", "tradeoffs": "safe, but it waits for a person"}
  ],
  "recommendation": {"option_id": "o1", "rationale": "one retry is cheap"},
  "default": "hold",
  "risk": "low",
  "priority": "high",
  "state": "open",
  "created_at": "2026-01-02T03:04:05Z"
}`

const testCurrentAnswerJSON = `{
  "id": "an_1",
  "chosen_option_id": "o1",
  "quote": "retry once, then stop",
  "actor": "alice",
  "surface": "cli",
  "created_at": "2026-01-02T09:30:00Z"
}`

const testAnswerLogJSON = `{
  "answers": [
    {"id": "an_0", "quote": "not yet, hold", "actor": "bob", "superseded": true, "superseded_by": "an_1"},
    {"id": "an_1", "chosen_option_id": "o1", "quote": "retry once, then stop", "actor": "alice", "superseded": false}
  ],
  "current_answer_id": "an_1"
}`

// questionRouteDaemon serves per-route replies and records every request
// (method, path, query, body).
type questionRouteDaemon struct {
	requests []questionRouteRequest
	routes   map[string]questionRouteReply // key: "METHOD /path"
}

type questionRouteRequest struct {
	method string
	path   string
	query  string
	body   []byte
}

type questionRouteReply struct {
	code int
	body string
}

func (d *questionRouteDaemon) bodyJSON(t *testing.T, i int) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(d.requests[i].body, &m); err != nil {
		t.Fatalf("request %d body %q is not JSON: %v", i, d.requests[i].body, err)
	}
	return m
}

func (d *questionRouteDaemon) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.requests = append(d.requests, questionRouteRequest{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   raw,
		})
		rep, ok := d.routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected route ` + r.URL.Path + `"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if rep.code != 0 {
			w.WriteHeader(rep.code)
		}
		_, _ = w.Write([]byte(rep.body))
	}
}

// withQuestionRouteServer points the CLI at the fake daemon.
func withQuestionRouteServer(t *testing.T, d *questionRouteDaemon) {
	t.Helper()
	old := serverURL
	srv := httptest.NewServer(d.handler())
	t.Cleanup(srv.Close)
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
}

// runQuestionCmd executes `question <args…>` on a root command and returns
// the combined output and the error (a non-nil error is a non-zero exit for
// the operator).
func runQuestionCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newQuestionCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"question"}, args...))
	err := root.Execute()
	return out.String(), err
}

// wantContains fails the test unless every want string is in out.
func wantContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// ----- list ----------------------------------------------------------------

func TestQuestionListDefaultsToOpenState(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/questions": {body: `{"count":1,"questions":[` + testQuestionJSON + `]}`},
	}}
	withQuestionRouteServer(t, d)

	out, err := runQuestionCmd(t, "list")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	if len(d.requests) != 1 || d.requests[0].method != "GET" || d.requests[0].path != "/api/questions" {
		t.Fatalf("requests = %+v, want one GET /api/questions", d.requests)
	}
	q, _ := url.ParseQuery(d.requests[0].query)
	if got := q.Get("state"); got != "open" {
		t.Errorf("state filter = %q, want the default open", got)
	}
	wantContains(t, out,
		"q_1",
		"open",
		"decision",
		"Which recovery path",
		"asked by: sup-1 (supervisor)",
		"priority=high",
	)
}

func TestQuestionListSendsTheFilters(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/questions": {body: `{"count":0,"questions":[]}`},
	}}
	withQuestionRouteServer(t, d)

	out, err := runQuestionCmd(t, "list", "--state", "all", "--context", "prd", "--asker", "sup-1", "--text", "recovery", "--limit", "7")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	q, _ := url.ParseQuery(d.requests[0].query)
	for k, want := range map[string]string{
		"state":   "all",
		"context": "prd",
		"asker":   "sup-1",
		"text":    "recovery",
		"limit":   "7",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q (query %q)", k, got, want, d.requests[0].query)
		}
	}
	if !strings.Contains(out, "no questions match") {
		t.Errorf("empty list should say no questions match:\n%s", out)
	}
}

func TestQuestionListRejectsAnUnknownState(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "list", "--state", "bogus")
	if err == nil || !strings.Contains(err.Error(), "--state") {
		t.Fatalf("want a --state error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for an unknown state")
	}
}

func TestQuestionListJSONPrintsTheRawBody(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/questions": {body: `{"count":1,"questions":[` + testQuestionJSON + `]}`},
	}}
	withQuestionRouteServer(t, d)

	// --json goes through prettyPrint, which writes to the process stdout:
	// capture os.Stdout around the run.
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = writer
	_, runErr := runQuestionCmd(t, "list", "--json")
	writer.Close()
	os.Stdout = oldStdout
	capBuf, _ := io.ReadAll(reader)
	if runErr != nil {
		t.Fatalf("err: %v (stdout: %s)", runErr, capBuf)
	}
	out := string(capBuf)
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("want the raw JSON body, got:\n%s", out)
	}
	wantContains(t, out, `"count": 1`, `"options"`, `"q_1"`)
}

// ----- show ----------------------------------------------------------------

func TestQuestionShowPrintsQuoteAndSupersededFlag(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/questions/q_1":         {body: `{"question":` + testQuestionJSON + `,"answer_count":2,"current_answer_id":"an_1","answer":` + testCurrentAnswerJSON + `}`},
		"GET /api/questions/q_1/answers": {body: testAnswerLogJSON},
	}}
	withQuestionRouteServer(t, d)

	out, err := runQuestionCmd(t, "show", "q_1")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	if len(d.requests) != 2 {
		t.Fatalf("requests = %+v, want the question and its answer log", d.requests)
	}
	wantContains(t, out,
		"q_1  state: open",
		"Which recovery path",
		"asked by: sup-1 (supervisor)",
		"priority: high",
		"risk: low",
		"1. o1  Retry now",
		"fast, but it may double the work",
		"2. o2  Escalate to an operator",
		"recommendation: Retry now",
		"one retry is cheap",
		"default: hold",
		"current answer:",
		`quote: "retry once, then stop"`,
		"by: alice (cli)",
		"answer log (2):",
		"an_0  superseded (by an_1)",
		`quote: "not yet, hold"`,
		"an_1  current",
	)
}

func TestQuestionShowUnknownQuestionFails(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/questions/q_missing": {code: http.StatusNotFound, body: `{"error":"unknown question"}`},
	}}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "show", "q_missing")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want a 404 error, got %v", err)
	}
}

// ----- answer --------------------------------------------------------------

func TestQuestionAnswerNeedsAnOptionOrText(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "answer", "q_1", "--text", "   ")
	if err == nil || !strings.Contains(err.Error(), "--option") {
		t.Fatalf("want an option-or-text error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for an empty answer")
	}
}

func TestQuestionAnswerSendsTheWordsVerbatimAndPrintsTheLabel(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"POST /api/questions/q_1/answer": {code: http.StatusCreated,
			body: `{"answer":` + testCurrentAnswerJSON + `,"question":` + testQuestionJSON + `}`},
	}}
	withQuestionRouteServer(t, d)

	words := "retry once,  please  (keep my spacing)"
	out, err := runQuestionCmd(t, "answer", "q_1", "--option", "o1", "--text", words)
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	body := d.bodyJSON(t, 0)
	if got := body["quote"]; got != words {
		t.Errorf("quote = %q, want the words exactly as given %q", got, words)
	}
	if body["chosen_option_id"] != "o1" || body["surface"] != "cli" {
		t.Errorf("body = %v, want the option id and surface cli", body)
	}
	if actor, _ := body["actor"].(string); actor == "" {
		t.Error("the local login name must be the actor")
	}
	if _, has := body["supersedes"]; has {
		t.Errorf("no supersedes for the first answer: %v", body)
	}
	wantContains(t, out,
		"option: Retry now",
		`quote: "retry once, then stop"`,
		"question is now: open",
	)
}

func TestQuestionAnswerTextOnlyIsAllowed(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"POST /api/questions/q_1/answer": {code: http.StatusCreated,
			body: `{"answer":{"id":"an_1","quote":"no, do neither and tell me what you see instead","actor":"alice","surface":"cli"},"question":` + testQuestionJSON + `}`},
	}}
	withQuestionRouteServer(t, d)

	out, err := runQuestionCmd(t, "answer", "q_1", "--text", "no, do neither and tell me what you see instead")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	body := d.bodyJSON(t, 0)
	if _, has := body["chosen_option_id"]; has {
		t.Errorf("text-only answer must not carry an option: %v", body)
	}
	wantContains(t, out, `quote: "no, do neither and tell me what you see instead"`)
}

func TestQuestionAnswerOverASupersededAnswerSendsSupersedes(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"POST /api/questions/q_1/answer": {code: http.StatusCreated,
			body: `{"answer":{"id":"an_2","chosen_option_id":"o2","supersedes":"an_1","actor":"alice","surface":"cli"},"question":` + testQuestionJSON + `}`},
	}}
	withQuestionRouteServer(t, d)

	out, err := runQuestionCmd(t, "answer", "q_1", "--option", "o2", "--text", "changed my mind", "--supersedes", "an_1")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	body := d.bodyJSON(t, 0)
	if got := body["supersedes"]; got != "an_1" {
		t.Errorf("supersedes = %v, want an_1 (the answer being superseded)", got)
	}
	if got := body["quote"]; got != "changed my mind" {
		t.Errorf("quote = %v, want it verbatim", got)
	}
	wantContains(t, out, "option: Escalate to an operator")
}

// ----- withdraw ------------------------------------------------------------

func TestQuestionWithdrawNeedsAReason(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "withdraw", "q_1")
	if err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("want a --reason error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for a reasonless withdraw")
	}
}

func TestQuestionWithdrawSendsTheReason(t *testing.T) {
	withdrawn := `{"id":"q_1","state":"withdrawn","text":"Which recovery path should the Automaton take?"}`
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"POST /api/questions/q_1/withdraw": {body: `{"question":` + withdrawn + `}`},
	}}
	withQuestionRouteServer(t, d)

	out, err := runQuestionCmd(t, "withdraw", "q_1", "--reason", "the recovery path is already done")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	if d.requests[0].method != "POST" || d.requests[0].path != "/api/questions/q_1/withdraw" {
		t.Fatalf("request = %+v, want POST /api/questions/q_1/withdraw", d.requests[0])
	}
	body := d.bodyJSON(t, 0)
	if got := body["reason"]; got != "the recovery path is already done" {
		t.Errorf("reason = %v, want it verbatim", got)
	}
	wantContains(t, out, "question q_1 withdrawn")
}

// ----- raise ---------------------------------------------------------------

func TestQuestionRaiseNeedsTwoOptions(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "raise", "--text", "t", "--option", "o1=Label", "--default", "hold")
	if err == nil || !strings.Contains(err.Error(), "two --option") {
		t.Fatalf("want a two-options error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for a one-option question")
	}
}

func TestQuestionRaiseOptionPairsMustBeIDEqualsLabel(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "raise", "--text", "t", "--option", "no-equals-sign", "--option", "o2=Label", "--default", "hold")
	if err == nil || !strings.Contains(err.Error(), "id=label") {
		t.Fatalf("want an id=label error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for a malformed option")
	}
}

func TestQuestionRaiseNeedsADefault(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "raise", "--text", "t", "--option", "o1=A", "--option", "o2=B")
	if err == nil || !strings.Contains(err.Error(), "--default") {
		t.Fatalf("want a --default error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for a question with no default")
	}
}

func TestQuestionRaiseSendsTheFullBody(t *testing.T) {
	raised := `{"id":"q_new","label":"decision","state":"open","asked_by":"sup-1","asker_kind":"supervisor","default":"o1","options":[{"id":"o1","label":"Retry now"},{"id":"o2","label":"Escalate to an operator"}]}`
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"POST /api/questions": {code: http.StatusCreated, body: `{"question":` + raised + `}`},
	}}
	withQuestionRouteServer(t, d)

	out, err := runQuestionCmd(t, "raise",
		"--text", "Which recovery path should the Automaton take?",
		"--label", "recovery-path",
		"--option", "o1=Retry now",
		"--option", "o2=Escalate to an operator",
		"--recommend", "o1",
		"--rationale", "one retry is cheap",
		"--default", "o1",
		"--risk", "low",
		"--priority", "high",
		"--context", "prd",
		"--prd-id", "prd-1",
		"--asker", "sup-1",
		"--asker-kind", "supervisor",
		"--session", "sess-1",
		"--return-path",
	)
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	if d.requests[0].method != "POST" || d.requests[0].path != "/api/questions" {
		t.Fatalf("request = %+v, want POST /api/questions", d.requests[0])
	}
	body := d.bodyJSON(t, 0)
	if body["text"] != "Which recovery path should the Automaton take?" || body["label"] != "recovery-path" {
		t.Errorf("text/label = %v %v", body["text"], body["label"])
	}
	if body["context"] != "prd" || body["prd_id"] != "prd-1" {
		t.Errorf("context link = %v %v, want prd context with prd-1", body["context"], body["prd_id"])
	}
	if body["default"] != "o1" || body["risk"] != "low" || body["priority"] != "high" {
		t.Errorf("default/risk/priority = %v %v %v", body["default"], body["risk"], body["priority"])
	}
	options, ok := body["options"].([]any)
	if !ok || len(options) != 2 {
		t.Fatalf("options = %v, want two options", body["options"])
	}
	rec, _ := body["recommendation"].(map[string]any)
	if rec["option_id"] != "o1" || rec["rationale"] != "one retry is cheap" {
		t.Errorf("recommendation = %v", body["recommendation"])
	}
	if body["asked_by"] != "sup-1" || body["asker_kind"] != "supervisor" {
		t.Errorf("asker = %v %v", body["asked_by"], body["asker_kind"])
	}
	if body["session_id"] != "sess-1" || body["return_path"] != true {
		t.Errorf("session = %v, return_path = %v", body["session_id"], body["return_path"])
	}
	wantContains(t, out, "question raised: q_new", "options: 2", "default: Retry now")
}

func TestQuestionRaiseContextNeedsItsLink(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "raise", "--text", "t", "--option", "o1=A", "--option", "o2=B", "--default", "hold", "--context", "prd")
	if err == nil || !strings.Contains(err.Error(), "--prd-id") {
		t.Fatalf("want a prd-link error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for a prd question with no prd link")
	}
}

func TestQuestionRaiseReturnPathNeedsASession(t *testing.T) {
	d := &questionRouteDaemon{}
	withQuestionRouteServer(t, d)
	_, err := runQuestionCmd(t, "raise", "--text", "t", "--option", "o1=A", "--option", "o2=B", "--default", "hold", "--return-path")
	if err == nil || !strings.Contains(err.Error(), "--session") {
		t.Fatalf("want a session error, got %v", err)
	}
	if len(d.requests) != 0 {
		t.Error("request reached the daemon for a return-path question with no session")
	}
}

// ----- the disabled switch -------------------------------------------------

func TestQuestionDisabledSwitchSurfacesTheDaemonMessage(t *testing.T) {
	msg := "operator questions are disabled: set autonomous.questions.enabled to true"
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/questions": {code: http.StatusServiceUnavailable, body: `{"error":` + jsonQuote(msg) + `}`},
	}}
	withQuestionRouteServer(t, d)

	_, err := runQuestionCmd(t, "list")
	if err == nil {
		t.Fatal("a 503 from the daemon must make the command fail (non-zero exit)")
	}
	if !strings.Contains(err.Error(), msg) {
		t.Errorf("the daemon's message must surface: %q in %v", msg, err)
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestQuestionGroupExposesAllSubcommands(t *testing.T) {
	cmd := newQuestionCmd()
	have := map[string]bool{}
	for _, sub := range cmd.Commands() {
		have[sub.Name()] = true
	}
	for _, want := range []string{"list", "show", "answer", "withdraw", "raise"} {
		if !have[want] {
			t.Errorf("the question group lacks the %q subcommand", want)
		}
	}
}
