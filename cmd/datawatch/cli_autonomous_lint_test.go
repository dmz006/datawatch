package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// lintSampleConfig is a live decompose-lint config snapshot as the daemon's
// GET /api/autonomous/decompose-lint/config returns it.
const lintSampleConfig = `{
  "decompose_lint": {
    "mode": "shadow",
    "max_retries": 4,
    "rules": [
      {"id": "bare_verbs", "severity": "warn", "patterns": ["^add "], "message": "task spec starts with a bare verb"}
    ],
    "disabled_rules": []
  }
}`

// lintSampleReport is the daemon's dry-run reply (POST
// /api/autonomous/prds/{id}/decompose-lint).
const lintSampleReport = `{
  "prd": "p1",
  "mode": "shadow",
  "rule_count": 6,
  "findings": [
    {"rule_id": "bare_verbs", "severity": "warn", "message": "task spec starts with a bare verb", "story": "s1", "task": "t1", "matched": "Add"},
    {"rule_id": "no_test_mention", "severity": "block", "message": "task spec never says what test proves it", "story": "s1", "task": "t2", "matched": ""}
  ],
  "summary": {"block": 1, "warn": 1}
}`

// lintSampleRecord is the daemon's stored-record reply (GET
// /api/autonomous/prds/{id}/decompose-lint).
const lintSampleRecord = `{
  "prd_id": "p1",
  "decompose_lint": {
    "decompose_lint_findings": [
      {"rule_id": "bare_verbs", "severity": "warn", "message": "task spec starts with a bare verb", "story": "s1", "task": "t1", "matched": "Add"}
    ],
    "mode": "enforce",
    "attempts": 2,
    "linted_at": "2026-11-01T10:00:00Z"
  }
}`

const lintNoRecord = `{"prd_id": "p9", "decompose_lint": null}`

// lintCLICall is one request the fake daemon recorded.
type lintCLICall struct {
	method, path, body, auth string
}

// newLintCLIDaemon runs a fake daemon that records every request (method,
// path, body, Authorization header) and answers with handler, and points
// the CLI at it through a temporary HOME — the same wiring the lane-fill
// CLI tests use. The cleanup step asserts every call carried the daemon's
// exact "Bearer <token>" auth: the daemon strips the "Bearer " prefix, so
// any other capitalization 401s against the real daemon and is caught
// here instead of in production.
func newLintCLIDaemon(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body []byte)) *[]lintCLICall {
	t.Helper()
	var calls []lintCLICall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		calls = append(calls, lintCLICall{method: r.Method, path: r.URL.Path, body: string(raw), auth: r.Header.Get("Authorization")})
		handler(w, r, raw)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DATAWATCH_TOKEN", "linttok")
	t.Cleanup(func() {
		for i, c := range calls {
			if c.auth != "Bearer linttok" {
				t.Errorf("call %d sent Authorization %q, want \"Bearer linttok\"", i, c.auth)
			}
		}
	})
	return &calls
}

func lintJSONReply(body string) func(w http.ResponseWriter, r *http.Request, body2 []byte) {
	return func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_, _ = w.Write([]byte(body))
	}
}

// runLintCmd runs the lint cobra command with argv and returns what it
// printed (and what it returned). The daemon-facing helpers (prettyPrint
// and the RunE prints) write to os.Stdout rather than cobra's output
// stream, so the capture is at the OS level, the way the other
// daemon-facing CLI tests do it.
func runLintCmd(t *testing.T, c *cobra.Command, argv []string) (string, error) {
	t.Helper()
	c.SetArgs(argv)
	var execErr error
	out := captureStdout(t, func() { execErr = c.Execute() })
	return out, execErr
}

func TestAutonomousLintCLI_Registered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	if !names["lint"] {
		t.Fatal("`lint` is not registered under `autonomous`")
	}
	sub := map[string]bool{}
	for _, c := range newAutonomousLintCmd().Commands() {
		sub[c.Name()] = true
	}
	for _, want := range []string{"config", "config-set", "dry-run", "findings"} {
		if !sub[want] {
			t.Errorf("the lint command lacks subcommand %q", want)
		}
	}
	long := newAutonomousCmd().Long
	for _, want := range []string{"lint config", "lint config-set", "lint dry-run <id>", "lint findings <id>"} {
		if !strings.Contains(long, want) {
			t.Errorf("the autonomous help block lacks %q:\n%s", want, long)
		}
	}
}

func TestAutonomousLintCLI_ConfigAgainstFakeDaemon(t *testing.T) {
	calls := newLintCLIDaemon(t, lintJSONReply(lintSampleConfig))
	out, err := runLintCmd(t, newAutonomousLintConfigCmd(), []string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodGet ||
		(*calls)[0].path != "/api/autonomous/decompose-lint/config" {
		t.Fatalf("calls = %+v, want one GET on the decompose-lint config endpoint", *calls)
	}
	for _, want := range []string{"\"mode\": \"shadow\"", "bare_verbs"} {
		if !strings.Contains(out, want) {
			t.Errorf("the config output lacks %q:\n%s", want, out)
		}
	}
}

func TestAutonomousLintCLI_ConfigJSONFlagPrintsRaw(t *testing.T) {
	newLintCLIDaemon(t, lintJSONReply(lintSampleConfig))
	out, err := runLintCmd(t, newAutonomousLintConfigCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	// --json is the raw daemon body: the daemon's own values, without any
	// operator-facing rewrite.
	if !strings.Contains(out, `"mode": "shadow"`) || !strings.Contains(out, "bare_verbs") {
		t.Fatalf("the --json output should be the daemon body:\n%s", out)
	}
}

func TestAutonomousLintCLI_ConfigSetSendsMergedBlock(t *testing.T) {
	// The fake daemon answers with the live block on the GET and echoes
	// the PUT body back on the PUT — like the real daemon's "here is the
	// new config" reply.
	respond := func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Method == http.MethodPut {
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write([]byte(lintSampleConfig))
	}
	calls := newLintCLIDaemon(t, respond)
	out, err := runLintCmd(t, newAutonomousLintConfigSetCmd(), []string{"mode=enforce", "max_retries=2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %+v, want a GET then a PUT", *calls)
	}
	if (*calls)[0].method != http.MethodGet || (*calls)[1].method != http.MethodPut {
		t.Fatalf("methods = %s, %s, want GET then PUT", (*calls)[0].method, (*calls)[1].method)
	}
	put := (*calls)[1].body
	for _, want := range []string{
		`"decompose_lint"`,    // framed as the decompose_lint block
		`"mode":"enforce"`,
		`"max_retries":2`,
		"bare_verbs",    // the live rule pack the override did not touch
		`"disabled_rules"`, // the untouched field passes through
	} {
		if !strings.Contains(put, want) {
			t.Errorf("the PUT body lacks %q:\n%s", want, put)
		}
	}
	if !strings.Contains(out, "enforce") {
		t.Errorf("the reply lacks the new mode:\n%s", out)
	}
}

func TestAutonomousLintCLI_ConfigSetRejectsBadPairs(t *testing.T) {
	calls := newLintCLIDaemon(t, lintJSONReply(lintSampleConfig))
	for i, args := range [][]string{
		{"mode=bogus"},
		{"max_retries=-1"},
		{"max_retries=many"},
		{"nonsense=1"},
		{},
	} {
		if _, err := runLintCmd(t, newAutonomousLintConfigSetCmd(), args); err == nil {
			t.Errorf("case %d (%v): expected an error", i, args)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("bad pairs must be rejected before any request; daemon saw %+v", *calls)
	}
}

func TestAutonomousLintCLI_DryRunRendersFindings(t *testing.T) {
	calls := newLintCLIDaemon(t, lintJSONReply(lintSampleReport))
	out, err := runLintCmd(t, newAutonomousLintDryRunCmd(), []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodPost ||
		(*calls)[0].path != "/api/autonomous/prds/p1/decompose-lint" {
		t.Fatalf("calls = %+v, want one POST on the per-PRD decompose-lint endpoint", *calls)
	}
	for _, want := range []string{
		"[warn] bare_verbs t1: task spec starts with a bare verb",
		"[block] no_test_mention t2: task spec never says what test proves it",
		"mode shadow",
		"1 block, 1 warn",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry-run output lacks %q:\n%s", want, out)
		}
	}
}

func TestAutonomousLintCLI_DryRunJSONFlagPrintsRaw(t *testing.T) {
	newLintCLIDaemon(t, lintJSONReply(lintSampleReport))
	out, err := runLintCmd(t, newAutonomousLintDryRunCmd(), []string{"p1", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\"rule_id\": \"bare_verbs\"") || !strings.Contains(out, `"summary"`) {
		t.Errorf("the --json output should be the daemon report body:\n%s", out)
	}
}

func TestAutonomousLintCLI_FindingsRendersStoredRecord(t *testing.T) {
	calls := newLintCLIDaemon(t, lintJSONReply(lintSampleRecord))
	out, err := runLintCmd(t, newAutonomousLintFindingsCmd(), []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodGet ||
		(*calls)[0].path != "/api/autonomous/prds/p1/decompose-lint" {
		t.Fatalf("calls = %+v, want one GET on the per-PRD decompose-lint endpoint", *calls)
	}
	for _, want := range []string{
		"[warn] bare_verbs t1: task spec starts with a bare verb",
		"mode enforce",
		"2 attempts",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the findings output lacks %q:\n%s", want, out)
		}
	}
}

func TestAutonomousLintCLI_FindingsNoRecord(t *testing.T) {
	newLintCLIDaemon(t, lintJSONReply(lintNoRecord))
	out, err := runLintCmd(t, newAutonomousLintFindingsCmd(), []string{"p9"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no decompose-lint record stored for p9") {
		t.Errorf("the no-record output lacks the shared no-record wording:\n%s", out)
	}
}

func TestAutonomousLintCLI_DryRunNeedsAnID(t *testing.T) {
	calls := newLintCLIDaemon(t, lintJSONReply(lintSampleReport))
	if _, err := runLintCmd(t, newAutonomousLintDryRunCmd(), nil); err == nil {
		t.Error("dry-run without an id must fail")
	}
	if len(*calls) != 0 {
		t.Errorf("dry-run without an id must not talk to the daemon; daemon saw %+v", *calls)
	}
}

func TestAutonomousLintCLI_FindingsNeedsAnID(t *testing.T) {
	calls := newLintCLIDaemon(t, lintJSONReply(lintSampleRecord))
	if _, err := runLintCmd(t, newAutonomousLintFindingsCmd(), nil); err == nil {
		t.Error("findings without an id must fail")
	}
	if len(*calls) != 0 {
		t.Errorf("findings without an id must not talk to the daemon; daemon saw %+v", *calls)
	}
}
