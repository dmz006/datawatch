// CLI coverage for `automation status` printing each row's pending member
// inserts below the table and showing held inserts in the PENDING column.
package main

import (
	"net/http"
	"strings"
	"testing"
)

// statusBodyOneInsert is a GET /api/autonomous/status reply: one Automation
// with one held insert (counted in pending_members) and one Automation with
// no pending inserts at all.
const statusBodyOneInsert = `{"automations":[
{"id":"ab12","name":"chain","status":"running","current_member_id":"m1","current_prd_title":"Do it",
 "pending_members":2,"pending_inserts":[
   {"id":"9c30ef8e","prd_id":"p-77","reason":"tests found a gap","actor":"agent:7","status":"pending"}]},
{"id":"o77d","name":"quiet","status":"paused","pending_members":0,"pending_inserts":[]}]}`

func TestAutomationCLIStatusShowsPendingInserts(t *testing.T) {
	c := &captureDaemon{replyBody: statusBodyOneInsert}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status") })
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if c.gotMethod != http.MethodGet || c.gotPath != "/api/autonomous/status" {
		t.Errorf("proxied %s %s, want GET /api/autonomous/status", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{
		"Pending inserts of ab12 (chain):",
		"9c30ef8e",
		"p-77",
		"tests found a gap",
		"datawatch automation approve ab12 9c30ef8e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// The row without pending inserts prints nothing extra: no per-row
	// pending section and no approve command naming it.
	if strings.Contains(out, "Pending inserts of o77d") {
		t.Errorf("row without inserts got a pending section:\n%s", out)
	}
	if strings.Contains(out, "approve o77d") {
		t.Errorf("row without inserts suggests an approve command:\n%s", out)
	}

	// A second Automation with inserts gets its own section too.
	c.replyBody = `{"automations":[
{"id":"ab12","name":"chain","status":"running","pending_members":1,
 "pending_inserts":[{"id":"9c30ef8e","prd_id":"p-77","reason":"tests found a gap","actor":"agent:7","status":"pending"}]},
{"id":"o77d","name":"quiet","status":"paused","pending_members":1,
 "pending_inserts":[{"id":"d1f0a0be","prd_id":"p-78","reason":"operator asked","actor":"dmz","status":"pending"}]}]}`
	out = captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status") })
	if err != nil {
		t.Fatalf("status (two rows with inserts): %v", err)
	}
	for _, want := range []string{
		"Pending inserts of ab12 (chain):",
		"Pending inserts of o77d (quiet):",
		"datawatch automation approve o77d d1f0a0be",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestAutomationCLIStatusPendingColumnCountsInserts(t *testing.T) {
	c := &captureDaemon{replyBody: statusBodyOneInsert}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status") })
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	// The ab12 row carries pending_members=2 (the daemon now counts the
	// held insert in it); the PENDING column must print that value.
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "ab12") && strings.Contains(l, "running") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("output lacks the ab12 table row:\n%s", out)
	}
	// PENDING is the 5th column: a standalone "2" not part of any other token.
	if !strings.Contains(line, " 2") {
		t.Errorf("PENDING column of the ab12 row = %q, want 2 (inserts count): %q", line, out)
	}

	// And the o77d row must still read 0 pending.
	oLine := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "o77d") {
			oLine = l
		}
	}
	if oLine == "" {
		t.Fatalf("output lacks the o77d table row:\n%s", out)
	}
	if strings.Contains(oLine, " 2") {
		t.Errorf("o77d row shows a pending count it does not have: %q", oLine)
	}
}
