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
      {"id": "bare_verbs", "severity": "warn", "patterns": ["^add ", "^remove "], "message": "task spec starts with a bare verb"}
    ],
    "disabled_rules": []
  }
}`

// lintSampleReport is a dry-run report as the daemon's POST
// /api/autonomous/prds/{id}/decompose-lint returns it.
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

// lintSampleRecord is a stored-record body as the daemon's GET
// /api/autonomous/prds/{id}/decompose-lint returns it.
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

// lintCLICall is one recorded request to the fake daemon.
type lintCLICall struct {
	method, path, body, auth string
}

// lintCLIDaemon runs a fake daemon that records every request and answers
// with handler, and points the CLI at it through a temporary HOME — the
// same wiring the lane-fill CLI tests use, with per-request recording.
type lintCLIDaemon struct {
	calls []lintCLICall
}

func newLintCLIDaemon(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body []byte)) *lintCLIDaemon {
	t.Helper()
	d := &lintCLIDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.calls = append(d.calls, lintCLICall{method: r.Method, path: r.URL.Path, body: string(raw), auth: r.Header.Get("Authorization")})
		handler(w, r, raw)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// Every daemon API surface expects the exact "Bearer <token>" form
	// (fedAuthMiddleware does strings.TrimPrefix(auth, "Bearer ")), so the
	// fake records the Authorization header on each call and fails on
	// anything else — a wrong prefix in lintDo would 401 against the real
	// daemon and is caught here.
	t.Setenv("DATAWATCH_TOKEN", "linttok")
	t.Cleanup(func() {
		for i, c := range d.calls {
			if c.auth != "Bearer linttok" {
				t.Errorf("call %d sent Authorization %q, want \"Bearer linttok\"", i, c.auth)
			}
		}
	})
	return d
}

func lintReplyText(t *testing.T, body string) func(w http.ResponseWriter, r *http.Request, body2 []byte) {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_, _ = w.Write([]byte(body))
	}
}

func runLintCmd(t *testing.T, c *cobra.Command, argv []string) (string, error) {
	return runLaneFillCmd(t, c, argv)
}

func TestAutonomousLintCLI_Registered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	if !names["lint"] {
		t.Fatal("lint is not registered under autonomous")
	}
	lint := newAutonomousLintCmd()
	sub := map[string]bool{}
	for _, c := range lint.Commands() {
		sub[c.Name()] = true
	}
	for _, want := range []string{"config", "config-set", "dry-run", "findings"} {
		if !sub[want] {
			t.Errorf("the lint command lacks subcommand %q", want)
		}
	}
	long := newAutonomousCmd().Long
	for _, want := range []string{
		"lint config",
		"lint dry-run <id>",
		"lint findings <id>",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("the autonomous help block lacks %q", want)
		}
	}
}

func TestAutonomousLintCLI_ConfigAgainstFakeDaemon(t *testing.T) {
	d := newLintCLIDaemon(t, lintReplyText(t, lintSampleConfig))
	out, err := runLintCmd(t, newAutonomousLintConfigCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0].method != http.MethodGet ||
		d.calls[0].path != "/api/autonomous/decompose-lint/config" {
		t.Fatalf("calls = %+v, want one GET on the decompose-lint config endpoint", d.calls)
	}
	for _, want := range []string{"\"mode\": \"shadow\"", "bare_verbs"} {
		if !strings.Contains(out, want) {
			t.Errorf("the config output lacks %q:\n%s", want, out)
		}
	}
}

func TestAutonomousLintCLI_ConfigSetSendsMergedBlock(t *testing.T) {
	// The fake daemon answers every request with the request body where it
	// makes sense (a PUT echoes the merged block, like the real daemon's
	// "here is the new config" reply), so the reply assertions hold.
	respond := func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Method == http.MethodPut {
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write([]byte(lintSampleConfig))
	}
	d := newLintCLIDaemon(t, respond)
	out, err := runLintCmd(t, newAutonomousLintConfigSetCmd(), []string{"mode=enforce", "max_retries=2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 2 {
		t.Fatalf("calls = %+v, want a GET then a PUT", d.calls)
	}
	if d.calls[0].method != http.MethodGet || d.calls[1].method != http.MethodPut {
		t.Fatalf("methods = %s, %s, want GET then PUT", d.calls[0].method, d.calls[1].method)
	}
	put := d.calls[1].body
	for _, want := range []string{
		`"mode":"enforce"`,
		`"max_retries":2`,
		"bare_verbs",       // the live rule pack the override did not touch
		`"decompose_lint"`, // framed as the decompose_lint block
		`"disabled_rules"`, // the untouched fields pass through
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
	d := newLintCLIDaemon(t, lintReplyText(t, lintSampleConfig))
	for i, args := range [][]string{
		{"mode=bogus"},
		{"max_retries=-1"},
		{"max_retries=many"},
		{"nonsense=1"},
	} {
		if _, err := runLintCmd(t, newAutonomousLintConfigSetCmd(), args); err == nil {
			t.Errorf("case %d (%v): expected an error", i, args)
		}
	}
	if len(d.calls) != 0 {
		t.Errorf("bad pairs must be rejected before any request; daemon saw %+v", d.calls)
	}
}

func TestAutonomousLintCLI_DryRunRendersFindings(t *testing.T) {
	d := newLintCLIDaemon(t, lintReplyText(t, lintSampleReport))
	out, err := runLintCmd(t, newAutonomousLintDryRunCmd(), []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0].method != http.MethodPost ||
		d.calls[0].path != "/api/autonomous/prds/p1/decompose-lint" {
		t.Fatalf("calls = %+v, want one POST on the per-PRD decompose-lint endpoint", d.calls)
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

func TestAutonomousLintCLI_FindingsRendersStoredRecord(t *testing.T) {
	d := newLintCLIDaemon(t, lintReplyText(t, lintSampleRecord))
	out, err := runLintCmd(t, newAutonomousLintFindingsCmd(), []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 1 || d.calls[0].method != http.MethodGet ||
		d.calls[0].path != "/api/autonomous/prds/p1/decompose-lint" {
		t.Fatalf("calls = %+v, want one GET on the per-PRD decompose-lint endpoint", d.calls)
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
	d := newLintCLIDaemon(t, lintReplyText(t, lintNoRecord))
	out, err := runLintCmd(t, newAutonomousLintFindingsCmd(), []string{"p9"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no decompose-lint record stored for p9") {
		t.Errorf("the no-record output lacks the no-record wording:\n%s", out)
	}
	if len(d.calls) != 1 || d.calls[0].path != "/api/autonomous/prds/p9/decompose-lint" {
		t.Fatalf("calls = %+v, want one GET on p9's decompose-lint endpoint", d.calls)
	}
}

func TestAutonomousLintCLI_DryRunNeedsAnID(t *testing.T) {
	d := newLintCLIDaemon(t, lintReplyText(t, lintSampleReport))
	if _, err := runLintCmd(t, newAutonomousLintDryRunCmd(), nil); err == nil {
		t.Error("dry-run without an id must fail")
	}
	if len(d.calls) != 0 {
		t.Errorf("dry-run without an id must not talk to the daemon; daemon saw %+v", d.calls)
	}
}
