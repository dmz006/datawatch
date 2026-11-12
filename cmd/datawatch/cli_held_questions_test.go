// CLI tests for `datawatch question held` (the held-question report read),
// against an httptest fake daemon (same pattern as cli_questions_test.go).

package main

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// heldOnReportBody is a full "on"-mode report: one open held question for
// prd-7 (held in awaiting_decision, recommended escalate, no forced
// default) and one in shadow mode, plus the five held state ids.
const heldOnReportBody = `{
  "mode": "on",
  "states": ["blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"],
  "open": [
    {
      "question_id": "q_held_1",
      "state": "awaiting_decision",
      "prd_id": "prd-7",
      "task_id": "t_9",
      "automation_id": "bot-scribe",
      "title": "Scribe Bot",
      "reason": "spec step 4 is ambiguous",
      "recommended_option": "escalate",
      "default": "escalate"
    },
    {
      "question_id": "q_held_2",
      "state": "blocked",
      "prd_id": "prd-3",
      "recommended_option": "continue",
      "default": "hold"
    }
  ],
  "shadow": [
    {"prd_id": "prd-7", "state": "pending_insert", "recommended_option": "escalate", "at": "2026-01-02T03:04:05Z"}
  ]
}`

const heldOffReportBody = `{
  "mode": "off",
  "states": ["blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"],
  "open": [],
  "shadow": []
}`

func withHeldRouteServer(t *testing.T, d *questionRouteDaemon) {
	t.Helper()
	withQuestionRouteServer(t, d)
}

func TestQuestionHeldIsInTheGroup(t *testing.T) {
	cmd := newQuestionCmd()
	names := map[string]bool{}
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	if !names["held"] {
		t.Error("the question group lacks the held subcommand")
	}
}

func TestQuestionHeldReadsTheReportAndPrintsTheMode(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/autonomous/held-questions": {body: heldOnReportBody},
	}}
	withHeldRouteServer(t, d)

	out, err := runQuestionCmd(t, "held")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	if len(d.requests) != 1 || d.requests[0].method != "GET" || d.requests[0].path != "/api/autonomous/held-questions" {
		t.Fatalf("requests = %+v, want one GET /api/autonomous/held-questions", d.requests)
	}
	if d.requests[0].query != "" {
		t.Errorf("no filters given, want an empty query, got %q", d.requests[0].query)
	}
	wantContains(t, out,
		"held mode: on",
		"blocked, review_held, revision_requested, awaiting_decision, pending_insert",
		"held questions are raised now",
		"q_held_1",
		"held: awaiting_decision",
		"bot-scribe (Scribe Bot)",
		"task t_9",
		"why: spec step 4 is ambiguous",
		"recommended: escalate (forced default)",
		"q_held_2",
		"held: blocked",
		"recommended: continue  (default: hold)",
		"shadow  pending_insert  prd-7",
		"recommended: escalate",
		"2026-01-02T03:04:05Z",
	)
}

func TestQuestionHeldSendsItsFilters(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/autonomous/held-questions": {body: heldOffReportBody},
	}}
	withHeldRouteServer(t, d)

	_, err := runQuestionCmd(t, "held", "--prd", "prd-7", "--state", "blocked")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	q, _ := url.ParseQuery(d.requests[0].query)
	if got := q.Get("prd_id"); got != "prd-7" {
		t.Errorf("prd_id = %q, want prd-7 (query %q)", got, d.requests[0].query)
	}
	if got := q.Get("state"); got != "blocked" {
		t.Errorf("state = %q, want blocked (query %q)", got, d.requests[0].query)
	}
}

func TestQuestionHeldOffModeSaysNothingIsHeld(t *testing.T) {
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/autonomous/held-questions": {body: heldOffReportBody},
	}}
	withHeldRouteServer(t, d)

	out, err := runQuestionCmd(t, "held")
	if err != nil {
		t.Fatalf("err: %v (output: %s)", err, out)
	}
	wantContains(t, out,
		"held mode: off",
		"blocked, review_held, revision_requested, awaiting_decision, pending_insert",
		"neither raises nor logs",
		"nothing on the daemon is held right now",
	)
}

func TestQuestionHeldDisabledSwitchSurfacesTheDaemonMessage(t *testing.T) {
	msg := "operator questions are disabled: set autonomous.questions.enabled to true first (held questions live under the operator-question subsystem switch)"
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/autonomous/held-questions": {code: http.StatusServiceUnavailable, body: `{"error":` + jsonQuote(msg) + `}`},
	}}
	withHeldRouteServer(t, d)

	_, err := runQuestionCmd(t, "held")
	if err == nil {
		t.Fatal("a 503 from the daemon must make the command fail (non-zero exit)")
	}
	if !strings.Contains(err.Error(), msg) {
		t.Errorf("the daemon's message must surface: %q in %v", msg, err)
	}
}

func TestQuestionHeldPassesThroughTheStateRejection(t *testing.T) {
	msg := `state must be one of the held question state ids: blocked, review_held, revision_requested, awaiting_decision, pending_insert (got "nope")`
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/autonomous/held-questions": {code: http.StatusBadRequest, body: `{"error":` + jsonQuote(msg) + `}`},
	}}
	withHeldRouteServer(t, d)

	_, err := runQuestionCmd(t, "held", "--state", "nope")
	if err == nil {
		t.Fatal("a 403/400 from the daemon must make the command fail (non-zero exit)")
	}
	if !strings.Contains(err.Error(), "pending_insert") {
		t.Errorf("the daemon's state vocabulary must surface: %v", err)
	}
}

func TestQuestionHeldJSONPrintsTheRawBody(t *testing.T) {
	if testing.Short() {
		t.Skip("stdout capture is overkill for -short")
	}
	d := &questionRouteDaemon{routes: map[string]questionRouteReply{
		"GET /api/autonomous/held-questions": {body: heldOnReportBody},
	}}
	withHeldRouteServer(t, d)

	// --json goes through prettyPrint, which writes to the process stdout:
	// capture os.Stdout around the run.
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = writer
	_, runErr := runQuestionCmd(t, "held", "--json")
	if closeErr := writer.Close(); closeErr != nil {
		t.Errorf("writer.Close: %v", closeErr)
	}
	os.Stdout = oldStdout
	capBuf, _ := io.ReadAll(reader)
	if runErr != nil {
		t.Fatalf("err: %v (stdout: %s)", runErr, capBuf)
	}
	out := string(capBuf)
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("want the raw JSON body, got:\n%s", out)
	}
	wantContains(t, out, `"mode": "on"`, `"q_held_1"`, `"shadow"`)
}
