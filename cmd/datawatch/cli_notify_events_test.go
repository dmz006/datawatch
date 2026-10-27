// CLI tests for the notify event list surface: `autonomous
// prd-set-notify-events` and the guardrail profile `--notify-events`
// flag on create/update. Reuses the captureDaemon / withCaptureServer /
// stringSliceOf helpers from cli_autonomous_subscriptions_test.go.
package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// TestNotifyEvents_SetsKindsForAutomaton covers the happy path: the
// command POSTs the comma-separated kinds (split, trimmed, order kept)
// to the per-Automaton set_notify_events action.
func TestNotifyEvents_SetsKindsForAutomaton(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"a1"}`}
	withCaptureServer(t, c)

	cmd := newAutonomousPRDSetNotifyEventsCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"a1", " prd.blocked , task.session_started "})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}

	if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/prds/a1/set_notify_events" {
		t.Fatalf("route = %s %s, want POST /api/autonomous/prds/a1/set_notify_events", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	kinds := stringSliceOf(t, body, "notify_events")
	if len(kinds) != 2 || kinds[0] != "prd.blocked" || kinds[1] != "task.session_started" {
		t.Errorf("notify_events = %v, want [prd.blocked task.session_started]", kinds)
	}
}

// TestNotifyEvents_SetWarningKinds covers the new opt-in warning kinds
// on the CLI: the command forwards scope_warning and guardrail_warn
// verbatim (they are not task.* statuses, so there is no shorthand
// expansion) — turning per-Automaton warnings on never blocks, it only
// widens what reaches the operator.
func TestNotifyEvents_SetWarningKinds(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"a1"}`}
	withCaptureServer(t, c)

	cmd := newAutonomousPRDSetNotifyEventsCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"a1", "scope_warning, guardrail_warn"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %s)", err, out)
	}
	if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/prds/a1/set_notify_events" {
		t.Fatalf("route = %s %s, want POST /api/autonomous/prds/a1/set_notify_events", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	kinds := stringSliceOf(t, body, "notify_events")
	if len(kinds) != 2 || kinds[0] != "scope_warning" || kinds[1] != "guardrail_warn" {
		t.Errorf("notify_events = %v, want [scope_warning guardrail_warn] (bare kinds, no task.* expansion)", kinds)
	}
}

// TestNotifyEvents_InheritAndOmittedSendEmptyList covers the two
// "clear" spellings: an omitted kinds argument and the word inherit
// both send an empty list (inherit from the next tier).
func TestNotifyEvents_InheritAndOmittedSendEmptyList(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"omitted", []string{"a1"}},
		{"inherit", []string{"a1", "inherit"}},
		{"inherit-case-insensitive", []string{"a1", "INHERIT"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDaemon{replyBody: `{"id":"a1"}`}
			withCaptureServer(t, c)

			cmd := newAutonomousPRDSetNotifyEventsCmd()
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v (output: %s)", err, out)
			}
			if c.gotMethod != http.MethodPost || c.gotPath != "/api/autonomous/prds/a1/set_notify_events" {
				t.Fatalf("route = %s %s", c.gotMethod, c.gotPath)
			}
			body := c.body(t)
			kinds, ok := body["notify_events"].([]any)
			if !ok {
				t.Fatalf("notify_events is %T, want array; body: %s", body["notify_events"], c.gotRaw)
			}
			if len(kinds) != 0 {
				t.Errorf("notify_events = %v, want empty list", kinds)
			}
		})
	}
}

// TestNotifyEvents_RejectsMissingID: the command needs an id.
func TestNotifyEvents_RejectsMissingID(t *testing.T) {
	cmd := newAutonomousPRDSetNotifyEventsCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a missing id")
	}
}

// TestNotifyEvents_400ErrorTextSurfaced: the daemon's 400 body (e.g. an
// unknown kind with the valid-kind list) must reach the operator
// unchanged.
func TestNotifyEvents_400ErrorTextSurfaced(t *testing.T) {
	badText := `unknown notify event kind "bogus"; valid kinds: prd.blocked, prd.completed`
	c := &captureDaemon{replyCode: http.StatusBadRequest, replyBody: badText}
	withCaptureServer(t, c)

	cmd := newAutonomousPRDSetNotifyEventsCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"a1", "bogus"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected an error for a 400 reply (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error %q should mention the 400 status", err)
	}
	if !strings.Contains(err.Error(), badText) {
		t.Errorf("error %q should carry the daemon's error text unchanged: %q", err, badText)
	}
}

// TestNotifyEvents_ProfileFlagOnlyWhenSupplied: on create and update
// the body carries notify_events only when --notify-events was supplied;
// inherit sends an empty array (clears the list) and a csv sends the
// split list.
func TestNotifyEvents_ProfileFlagOnlyWhenSupplied(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verb    string
		args    []string
		wantKey bool
		want    []string
	}{
		{"create omitted", "create", []string{"--name", "p1"}, false, nil},
		{"create supplied", "create", []string{"--name", "p1", "--notify-events", "prd.failed,task.blocked"}, true, []string{"prd.failed", "task.blocked"}},
		{"create inherit", "create", []string{"--name", "p1", "--notify-events", "inherit"}, true, []string{}},
		{"update omitted", "update", []string{"prof1", "--name", "p1"}, false, nil},
		{"update supplied", "update", []string{"prof1", "--name", "p1", "--notify-events", "prd.blocked"}, true, []string{"prd.blocked"}},
		{"update inherit", "update", []string{"prof1", "--name", "p1", "--notify-events", "inherit"}, true, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantMethod := http.MethodPost
			wantPath := "/api/autonomous/guardrail_profiles"
			if tc.verb == "update" {
				wantMethod = http.MethodPut
				wantPath = "/api/autonomous/guardrail_profiles/prof1"
			}
			c := &captureDaemon{replyBody: `{"id":"prof1"}`}
			withCaptureServer(t, c)

			// Execute through the profile root so cobra resolves the verb
			// subcommand the way the real CLI does.
			cmd := newGuardrailProfileCmd()
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs(append([]string{tc.verb}, tc.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v (output: %s)", err, out)
			}
			if c.gotMethod != wantMethod || c.gotPath != wantPath {
				t.Fatalf("route = %s %s, want %s %s", c.gotMethod, c.gotPath, wantMethod, wantPath)
			}
			body := c.body(t)
			got, present := body["notify_events"]
			if present != tc.wantKey {
				t.Fatalf("notify_events present = %v, want %v; body: %s", present, tc.wantKey, c.gotRaw)
			}
			if tc.wantKey {
				kinds, ok := got.([]any)
				if !ok {
					t.Fatalf("notify_events is %T, want array; body: %s", got, c.gotRaw)
				}
				if len(kinds) != len(tc.want) {
					t.Errorf("notify_events = %v, want %v", kinds, tc.want)
					return
				}
				for i, w := range tc.want {
					if kinds[i] != w {
						t.Errorf("notify_events[%d] = %v, want %v", i, kinds[i], w)
					}
				}
			}
		})
	}
}

// TestNotifyEvents_HelpListsValidKindsAndDefaults guards the help text
// against drifting from the daemon's accepted kind set: every valid
// kind and every default-set member must be named.
func TestNotifyEvents_HelpListsValidKindsAndDefaults(t *testing.T) {
	long := newAutonomousPRDSetNotifyEventsCmd().Long
	for _, kind := range autonomouspkg.ValidNotifyEventKinds() {
		if !strings.Contains(long, kind) {
			t.Errorf("help should list valid kind %q:\n%s", kind, long)
		}
	}
	for _, kind := range autonomouspkg.DefaultNotifyEvents() {
		if !strings.Contains(long, kind) {
			t.Errorf("help should list default kind %q:\n%s", kind, long)
		}
	}
	if !strings.Contains(long, "inherit") {
		t.Errorf("help should document the inherit (clear) spelling:\n%s", long)
	}
	if !strings.Contains(newAutonomousCmd().Long, "prd-set-notify-events") {
		t.Error("autonomous help should list the prd-set-notify-events subcommand")
	}
}
