// CLI tests for `datawatch autonomous decision-records` (list, show, create,
// update, withdraw), against an httptest fake daemon that records every
// request (method, path, query, body) and serves canned replies. The render
// functions are checked over real wire shapes the daemon uses
// (internal/server/decision_records.go): the record with its answer, document
// report and follow-up, plus the folder watch status on the list.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const decRecordListBody = `{
  "decision_records": [
    {
      "id": "dr_9",
      "label": "decision",
      "title": "Which build engine?",
      "situation": "We must pick a build engine before the launch.",
      "deciding": "What the Automaton builds and how it ships.",
      "options": [
        {"id": "go", "label": "Plain Go", "tradeoff": "familiar, but two toolchains"},
        {"id": "zig", "label": "Zig", "tradeoff": "one toolchain, less battle-tested"}
      ],
      "recommendation": {"option_id": "go", "reason": "the team knows it"},
      "default": "hold",
      "links": {"doc": "docs/build.md", "automaton": "prd-1"},
      "state": "answered",
      "question_id": "q_9",
      "question_state": "answered",
      "content_hash": "sha256:abc",
      "answer": {
        "answer_id": "an_1",
        "chosen_option_id": "go",
        "quote": "Go, ship it",
        "actor": "alice",
        "surface": "cli",
        "answered_at": "2026-01-03T10:00:00Z"
      },
      "doc_change": {
        "path": "docs/build.md",
        "before": "no engine",
        "after": "plain Go",
        "status": "applied",
        "at": "2026-01-03T10:05:00Z"
      },
      "follow_up": {
        "requested": true,
        "name": "kickoff",
        "kind": "session",
        "status": "done",
        "result": "build started"
      },
      "created_at": "2026-01-02T09:00:00Z",
      "updated_at": "2026-01-03T10:05:00Z"
    },
    {
      "id": "dr_8",
      "title": "Roll out nightly builds?",
      "situation": "Nightly builds have been asked for twice.",
      "deciding": "Whether the Automaton runs a nightly build.",
      "options": [
        {"id": "yes", "label": "Yes, nightly at 02:00"},
        {"id": "no", "label": "No, on demand only"}
      ],
      "default": "no",
      "state": "open",
      "question_id": "q_8",
      "question_state": "open"
    }
  ],
  "folder": {
    "enabled": true,
    "path": "docs/decisions",
    "files_seen": 4,
    "errors": [{"file": "docs/decisions/broken.md", "message": "not YAML"}],
    "notes": [],
    "last_scan": "2026-01-04T00:00:00Z"
  }
}`

// decRecordDaemon serves per-route canned replies and records every request.
type decRecordDaemon struct {
	requests []decRecordRequest
	routes   map[string]decRecordReply // key: "METHOD /path"
}

type decRecordRequest struct {
	method string
	path   string
	query  string
	body   []byte
}

type decRecordReply struct {
	code int
	body string
}

func (d *decRecordDaemon) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.requests = append(d.requests, decRecordRequest{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   raw,
		})
		rep, ok := d.routes[r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery]
		if !ok {
			rep, ok = d.routes[r.Method+" "+r.URL.Path]
		}
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected route"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if rep.code != 0 {
			w.WriteHeader(rep.code)
		}
		_, _ = w.Write([]byte(rep.body))
	}
}

func (d *decRecordDaemon) bodyJSON(t *testing.T, i int) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(d.requests[i].body, &m); err != nil {
		t.Fatalf("request %d body %q is not JSON: %v", i, d.requests[i].body, err)
	}
	return m
}

func withDecRecordServer(t *testing.T, d *decRecordDaemon) {
	t.Helper()
	old := serverURL
	srv := httptest.NewServer(d.handler())
	t.Cleanup(srv.Close)
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
}

// runDecRecordsCmd executes `autonomous decision-records <args…>` on a root
// command and returns the combined output and the error.
func runDecRecordsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "datawatch"}
	root.AddCommand(newDecisionRecordsCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"decision-records"}, args...))
	err := root.Execute()
	return out.String(), err
}

// ----- list -----------------------------------------------------------------

func TestDecisionRecordCLI_ListRendersAnswerDocumentAndFollowUp(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/autonomous/decision-records": {body: decRecordListBody},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "list")
	if err != nil {
		t.Fatalf("list failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"dr_9  answered  Which build engine?",
		"question: answered (q_9)",
		"answered with: Plain Go (id go)",
		"quote: \"Go, ship it\"",
		"by: alice (cli)",
		"document: applied — docs/build.md",
		"follow-up: kickoff [kind session",
		"— done: build started",
		"dr_8  open  Roll out nightly builds?",
		"question: open (q_8)",
		"folder watch: on, 4 file(s) seen, path docs/decisions",
		"error: docs/decisions/broken.md: not YAML",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output lacks %q:\n%s", want, out)
		}
	}
	// The open record has no answer block.
	if strings.Contains(out, "dr_8\n    answered") {
		t.Errorf("open record must not print an answer block:\n%s", out)
	}
	if len(d.requests) != 1 || d.requests[0].method != "GET" {
		t.Fatalf("want exactly one GET, got %+v", d.requests)
	}
}

func TestDecisionRecordCLI_ListEmptyPrintsPlainLine(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/autonomous/decision-records": {
			body: `{"decision_records":[],"folder":{"enabled":false,"errors":[],"notes":[]}}`,
		},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "list", "--state", "withdrawn")
	if err != nil {
		t.Fatalf("list failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no decision records match") {
		t.Fatalf("empty list lacks the plain line:\n%s", out)
	}
	if !strings.Contains(d.requests[0].query, "state=withdrawn") {
		t.Fatalf("state filter not sent: %q", d.requests[0].query)
	}
}

func TestDecisionRecordCLI_ListSendsAutomatonFilterAsPRDID(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/autonomous/decision-records": {body: `{"decision_records":[],"folder":{"enabled":false,"errors":[],"notes":[]}}`},
	}}
	withDecRecordServer(t, d)

	if _, err := runDecRecordsCmd(t, "list", "--automaton", "prd-7", "--limit", "5"); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	q := d.requests[0].query
	if !strings.Contains(q, "prd_id=prd-7") || !strings.Contains(q, "limit=5") {
		t.Fatalf("want prd_id and limit in the query, got %q", q)
	}
	if strings.Contains(q, "automaton=") {
		t.Fatalf("the daemon filter is prd_id, not automaton: %q", q)
	}
}

func TestDecisionRecordCLI_ListRejectsUnknownStateWithoutCalling(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "list", "--state", "bogus")
	if err == nil {
		t.Fatalf("unknown state must fail, got:\n%s", out)
	}
	if len(d.requests) != 0 {
		t.Fatalf("a refused state must not reach the daemon: %+v", d.requests)
	}
	if !strings.Contains(err.Error(), "open, answered or withdrawn") {
		t.Fatalf("error should name the allowed states: %v", err)
	}
}

func TestDecisionRecordCLI_ListJSONPrintsTheRawBody(t *testing.T) {
	raw := decRecordListBody
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/autonomous/decision-records": {body: raw},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "list", "--json")
	if err != nil {
		t.Fatalf("--json list failed: %v", err)
	}
	// prettyPrint re-encodes; check the raw fields came through.
	if !strings.Contains(out, `"dr_9"`) || !strings.Contains(out, `"folder"`) {
		t.Fatalf("--json output is not the raw body:\n%s", out)
	}
}

// ----- show -----------------------------------------------------------------

func TestDecisionRecordCLI_ShowRendersOneRecordInFull(t *testing.T) {
	record := `{
  "id": "dr_9", "title": "Which build engine?",
  "situation": "We must pick a build engine before the launch.",
  "deciding": "What the Automaton builds and how it ships.",
  "options": [{"id": "go", "label": "Plain Go", "tradeoff": "familiar, but two toolchains"},
               {"id": "zig", "label": "Zig", "tradeoff": "one toolchain, less battle-tested"}],
  "recommendation": {"option_id": "go", "reason": "the team knows it"},
  "default": "hold",
  "links": {"doc": "docs/build.md", "automaton": "prd-1"},
  "state": "answered", "question_id": "q_9", "question_state": "answered",
  "answer": {"answer_id": "an_1", "chosen_option_id": "go", "quote": "Go, ship it",
             "actor": "alice", "surface": "cli", "answered_at": "2026-01-03T10:00:00Z"},
  "doc_change": {"path": "docs/build.md", "before": "no engine", "after": "plain Go",
                 "status": "applied", "at": "2026-01-03T10:05:00Z"},
  "follow_up": {"requested": true, "name": "kickoff", "kind": "session",
                "status": "done", "result": "build started"}
}`
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/autonomous/decision-records/dr_9": {body: `{"record":` + record + `,"question_id":"q_9"}`},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "show", "dr_9")
	if err != nil {
		t.Fatalf("show failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"dr_9  state: answered",
		"title: Which build engine?",
		"We must pick a build engine before the launch.",
		"deciding: What the Automaton builds and how it ships.",
		"1. go  Plain Go",
		"cost: familiar, but two toolchains",
		"recommendation: Plain Go (id go): the team knows it",
		"default: hold",
		"links: doc docs/build.md, automaton prd-1",
		"question: q_9 (state: answered)",
		"option: Plain Go (id go)",
		"quote: \"Go, ship it\"",
		"by: alice (cli)",
		"document: applied",
		"follow-up:",
		"requested: kickoff",
		"result: build started",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output lacks %q:\n%s", want, out)
		}
	}
}

func TestDecisionRecordCLI_ShowUnknownRecordFails(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"GET /api/autonomous/decision-records/dr_x": {code: http.StatusNotFound, body: `{"error":"unknown decision record"}`},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "show", "dr_x")
	if err == nil {
		t.Fatalf("unknown record must fail, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "unknown decision record") {
		t.Fatalf("error should carry the daemon message: %v", err)
	}
}

// ----- create / update --------------------------------------------------------

func decRecordCreateFlags(args ...string) []string {
	base := []string{
		"--title", "Which build engine?",
		"--situation", "We must pick a build engine before the launch.",
		"--deciding", "What the Automaton builds and how it ships.",
		"--option", "go=Plain Go",
		"--option", "zig=Zig",
		"--tradeoff", "go=familiar, but two toolchains",
		"--recommend", "go", "--rationale", "the team knows it",
		"--default", "hold",
		"--doc", "docs/build.md",
	}
	return append(base, args...)
}

func TestDecisionRecordCLI_CreateSendsContentAndPrintsConfirmation(t *testing.T) {
	created := `{"record":{"id":"dr_50","state":"open","question_id":"q_50"},"question_id":"q_50","warnings":[]}`
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/autonomous/decision-records": {code: http.StatusCreated, body: created},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, append([]string{"create"}, decRecordCreateFlags()...)...)
	if err != nil {
		t.Fatalf("create failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "decision record raised") || !strings.Contains(out, "dr_50") {
		t.Fatalf("create confirmation lacks the id:\n%s", out)
	}
	if !strings.Contains(out, "question: q_50") {
		t.Fatalf("create confirmation lacks the question id:\n%s", out)
	}
	if len(d.requests) != 1 || d.requests[0].method != "POST" {
		t.Fatalf("want one POST to the collection, got %+v", d.requests)
	}
	b := d.bodyJSON(t, 0)
	if b["title"] != "Which build engine?" || b["default"] != "hold" {
		t.Fatalf("body content wrong: %v", b)
	}
	opts, _ := b["options"].([]any)
	if len(opts) != 2 {
		t.Fatalf("want two options, got %v", b["options"])
	}
	rec, _ := b["recommendation"].(map[string]any)
	if rec["option_id"] != "go" || rec["reason"] != "the team knows it" {
		t.Fatalf("recommendation wrong: %v", rec)
	}
	links, _ := b["links"].(map[string]any)
	if links["doc"] != "docs/build.md" {
		t.Fatalf("links wrong: %v", links)
	}
	// A body field the daemon would refuse must not sneak in.
	for _, forbidden := range []string{"state", "answer", "question_id"} {
		if _, has := b[forbidden]; has {
			t.Fatalf("create body must not name %q: %v", forbidden, b)
		}
	}
}

func TestDecisionRecordCLI_CreateTakesTheContentFromAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "record.json")
	content := `{
  "title": "Which build engine?",
  "situation": "Pick a build engine.",
  "deciding": "The engine of builds.",
  "options": [{"id":"go","label":"Plain Go","tradeoff":"two toolchains"},{"id":"zig","label":"Zig","tradeoff":"less tested"}],
  "default": "go"
}`
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/autonomous/decision-records": {code: http.StatusCreated,
			body: `{"record":{"id":"dr_51","state":"open"},"question_id":"q_51"}`},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "create", "--file", file)
	if err != nil {
		t.Fatalf("create --file failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "dr_51") {
		t.Fatalf("create confirmation:\n%s", out)
	}
	b := d.bodyJSON(t, 0)
	if b["default"] != "go" || b["title"] != "Which build engine?" {
		t.Fatalf("file content not sent: %v", b)
	}
}

func TestDecisionRecordCLI_CreateValidatesContentBeforeCalling(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{}}
	withDecRecordServer(t, d)

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing title", append([]string{"create"}, []string{"--situation", "s", "--deciding", "d", "--option", "a=x", "--option", "b=y", "--default", "a"}...), "needs --title"},
		{"one option", append([]string{"create"}, []string{"--title", "t", "--situation", "s", "--deciding", "d", "--option", "a=x", "--default", "a"}...), "at least two"},
		{"bad default", append([]string{"create"}, []string{"--title", "t", "--situation", "s", "--deciding", "d", "--option", "a=x", "--option", "b=y", "--default", "nope"}...), "neither an --option id nor hold"},
		{"recommend needs rationale", append([]string{"create"}, []string{"--title", "t", "--situation", "s", "--deciding", "d", "--option", "a=x", "--option", "b=y", "--default", "a", "--recommend", "a"}...), "needs --rationale"},
		{"tradeoff unknown option", append([]string{"create"}, []string{"--title", "t", "--situation", "s", "--deciding", "d", "--option", "a=x", "--option", "b=y", "--default", "a", "--tradeoff", "zz=text"}...), "a decision option is a, b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runDecRecordsCmd(t, tc.args...)
			if err == nil {
				t.Fatalf("must fail, got:\n%s", out)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q should say %q", err, tc.wantErr)
			}
		})
	}
	if len(d.requests) != 0 {
		t.Fatalf("none of the bad creates may reach the daemon: %+v", d.requests)
	}
}

func TestDecisionRecordCLI_UpdateSendsPutToTheRecordID(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"PUT /api/autonomous/decision-records/dr_9": {body: `{"record":{"id":"dr_9","state":"open"},"question_id":"q_9"}`},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, append([]string{"update", "dr_9"}, decRecordCreateFlags()...)...)
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "decision record updated") || !strings.Contains(out, "dr_9") {
		t.Fatalf("update confirmation:\n%s", out)
	}
	if len(d.requests) != 1 || d.requests[0].method != "PUT" || d.requests[0].path != "/api/autonomous/decision-records/dr_9" {
		t.Fatalf("want one PUT to the record, got %+v", d.requests)
	}
	if got := d.bodyJSON(t, 0)["title"]; got != "Which build engine?" {
		t.Fatalf("update body title = %v", got)
	}
}

func TestDecisionRecordCLI_UpdateClosedRecordSurfacesTheConflict(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"PUT /api/autonomous/decision-records/dr_9": {code: http.StatusConflict,
			body: `{"error":"this decision record is already answered (state: answered)"}`},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, append([]string{"update", "dr_9"}, decRecordCreateFlags()...)...)
	if err == nil {
		t.Fatalf("updating a closed record must fail, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("error should carry the daemon message: %v", err)
	}
}

// ----- withdraw ---------------------------------------------------------------

func TestDecisionRecordCLI_WithdrawSendsReasonAndPrintsConfirmation(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/autonomous/decision-records/dr_9/withdraw": {
			body: `{"record":{"id":"dr_9","state":"withdrawn","question_id":"q_9"},"question_id":"q_9"}`},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "withdraw", "dr_9", "--reason", "superseded by the registry decision")
	if err != nil {
		t.Fatalf("withdraw failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "dr_9 withdrawn") || !strings.Contains(out, "superseded by the registry decision") {
		t.Fatalf("withdraw confirmation:\n%s", out)
	}
	if len(d.requests) != 1 || d.requests[0].method != "POST" {
		t.Fatalf("want one POST to the withdraw route, got %+v", d.requests)
	}
	b := d.bodyJSON(t, 0)
	if b["reason"] != "superseded by the registry decision" {
		t.Fatalf("reason not sent: %v", b)
	}
}

func TestDecisionRecordCLI_WithdrawUnknownRecordFails(t *testing.T) {
	d := &decRecordDaemon{routes: map[string]decRecordReply{
		"POST /api/autonomous/decision-records/dr_x/withdraw": {
			code: http.StatusNotFound, body: `{"error":"unknown decision record"}`},
	}}
	withDecRecordServer(t, d)

	out, err := runDecRecordsCmd(t, "withdraw", "dr_x")
	if err == nil {
		t.Fatalf("withdrawing an unknown record must fail, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "unknown decision record") {
		t.Fatalf("error should carry the daemon message: %v", err)
	}
}
