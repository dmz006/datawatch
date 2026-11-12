// Phase 3a CLI coverage for `datawatch automation`: create with fork/join and
// nested members (inline JSON and from a file), the list and get verbs, and
// the activate verb's request, which must stay unchanged. Each test serves
// the daemon side with captureDaemon (see cli_autonomous_subscriptions_test.go).
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const cliFJMembersJSON = `[
  {"id":"plan","prd_id":"p-plan","on_complete":"fork1","on_blocked":"","on_failed":"","added_reason":"entry"},
  {"id":"fork1","kind":"fork","fork_members":["b1","n1"]},
  {"id":"b1","prd_id":"p-b1","on_complete":"join1","on_blocked":"","on_failed":""},
  {"id":"n1","kind":"automation","automation_id":"child-7","on_complete":"join1","on_blocked":"","on_failed":""},
  {"id":"join1","kind":"join","join_members":["b1","n1"],"on_branch_failure":"any_one_is_enough","on_complete":"","on_blocked":"","on_failed":""}
]`

// cliMembersEqual decodes the wire members and the sent JSON and compares them.
func cliMembersEqual(t *testing.T, got any, sent string) {
	t.Helper()
	var want []any
	if err := json.Unmarshal([]byte(sent), &want); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("members sent = %v, want %v", got, want)
	}
}

func TestAutomationCLICreateForkJoinAutomation(t *testing.T) {
	c := &captureDaemon{replyCode: http.StatusCreated, replyBody: `{"id":"ab12","status":"draft","members":[{},{},{},{},{}]}`}
	withCaptureServer(t, c)

	// Inline members JSON.
	var runErr error
	out := captureStdout(t, func() {
		runErr = runCmd(t, newAutomationCmd(), "create", "--name", "fj", "--entry-member", "plan", "--members", cliFJMembersJSON)
	})
	if runErr != nil {
		t.Fatalf("create inline: %v", runErr)
	}
	if c.gotMethod != "POST" || c.gotPath != "/api/automations" {
		t.Errorf("proxied %s %s", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	if body["name"] != "fj" || body["entry_member_id"] != "plan" {
		t.Errorf("body = %v", body)
	}
	cliMembersEqual(t, body["members"], cliFJMembersJSON)
	if want := "created automation ab12 (draft, 5 members)"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want %q", out, want)
	}

	// Members from a file, the nested-Automation shape included.
	path := filepath.Join(t.TempDir(), "members.json")
	if err := os.WriteFile(path, []byte(cliFJMembersJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		runErr = runCmd(t, newAutomationCmd(), "create", "--name", "fj-file", "--members", path)
	})
	if runErr != nil {
		t.Fatalf("create from file: %v", runErr)
	}
	cliMembersEqual(t, c.body(t)["members"], cliFJMembersJSON)

	// Bad input fails before any request is sent.
	c.gotPath = ""
	if err := runCmd(t, newAutomationCmd(), "create", "--name", "bad", "--members", "[not json"); err == nil ||
		!strings.Contains(err.Error(), "--members must be a JSON array") {
		t.Errorf("malformed inline members: err = %v", err)
	}
	if err := runCmd(t, newAutomationCmd(), "create", "--name", "bad", "--members", filepath.Join(t.TempDir(), "missing.json")); err == nil ||
		!strings.Contains(err.Error(), "read --members file") {
		t.Errorf("missing members file: err = %v", err)
	}
	if c.gotPath != "" {
		t.Errorf("request sent for invalid members: path=%q", c.gotPath)
	}
}

func TestAutomationCLIListAndGetVerbs(t *testing.T) {
	c := &captureDaemon{replyBody: `[` +
		`{"id":"ab12","name":"fj","status":"active","members":[{"id":"plan"},{"id":"fork1","kind":"fork"}],"active_cursors":["api","n1"]},` +
		`{"id":"cd34","name":"idle","status":"draft","members":[],"active_cursors":[]}]`}
	withCaptureServer(t, c)

	var runErr error
	out := captureStdout(t, func() { runErr = runCmd(t, newAutomationCmd(), "list") })
	if runErr != nil {
		t.Fatalf("list: %v", runErr)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/automations" {
		t.Errorf("list proxied %s %s", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{"ID", "ACTIVE", "ab12", "fj", "active", "api,n1", "cd34", "idle"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output lacks %q:\n%s", want, out)
		}
	}

	c.replyBody = `{"id":"ab12","name":"fj","status":"blocked","version":4,"entry_member_id":"plan",` +
		`"active_cursors":["api","n1"],"members":[` +
		`{"id":"plan","prd_id":"p-plan","on_complete":"fork1","status":"completed"},` +
		`{"id":"fork1","kind":"fork","fork_members":["b1","n1"]},` +
		`{"id":"b1","prd_id":"p-b1","on_complete":"join1"},` +
		`{"id":"n1","kind":"automation","automation_id":"child-7","on_complete":"join1"},` +
		`{"id":"join1","kind":"join","join_members":["b1","n1"],"on_branch_failure":"any_one_is_enough"}]}`
	out = captureStdout(t, func() { runErr = runCmd(t, newAutomationCmd(), "get", "ab12") })
	if runErr != nil {
		t.Fatalf("get: %v", runErr)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/automations/ab12" {
		t.Errorf("get proxied %s %s", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{
		"Active:   api,n1",
		"Entry:    plan",
		"prd",                                // default kind for a PRD member
		"p-b1",                               // PRD member target
		"branches b1,n1",                     // fork target
		"child-7",                            // nested Automation target
		"branches b1,n1 (any_one_is_enough)", // join target with its rule
		"stop",                               // no next member on the join
	} {
		if !strings.Contains(out, want) {
			t.Errorf("get output lacks %q:\n%s", want, out)
		}
	}

	// --json prints the REST body as it came back.
	out = captureStdout(t, func() { runErr = runCmd(t, newAutomationCmd(), "get", "ab12", "--json") })
	if runErr != nil {
		t.Fatalf("get --json: %v", runErr)
	}
	if strings.TrimSpace(out) != c.replyBody {
		t.Errorf("get --json = %q, want the raw body", out)
	}

	// The activate verb keeps its request: POST with no body fields when no
	// --actor or --reason is given.
	c.replyBody = `{"id":"ab12","status":"active"}`
	captureStdout(t, func() { runErr = runCmd(t, newAutomationCmd(), "activate", "ab12") })
	if runErr != nil {
		t.Fatalf("activate: %v", runErr)
	}
	if c.gotMethod != "POST" || c.gotPath != "/api/automations/ab12/activate" {
		t.Errorf("activate proxied %s %s", c.gotMethod, c.gotPath)
	}
	if got := strings.TrimSpace(string(c.gotRaw)); got != "{}" {
		t.Errorf("activate body = %q, want {}", got)
	}
}
