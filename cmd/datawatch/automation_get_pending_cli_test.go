// CLI coverage for `automation get` printing the Automation's pending
// member inserts and the exact approve command for each.
package main

import (
	"net/http"
	"strings"
	"testing"
)

const pendingInsertsOne = `{"id":"ab12","name":"chain","status":"paused","version":3,"members":[],
"pending_inserts":[{"id":"9c30ef8e","prd_id":"p-77","reason":"tests found a gap","actor":"agent:7","status":"pending"}]}`

func TestAutomationCLIGetShowsPendingInserts(t *testing.T) {
	c := &captureDaemon{replyBody: pendingInsertsOne}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "get", "ab12") })
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if c.gotMethod != http.MethodGet || c.gotPath != "/api/automations/ab12" {
		t.Errorf("proxied %s %s, want GET /api/automations/ab12", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{
		"Pending inserts: 1",
		"9c30ef8e",
		"p-77",
		"tests found a gap",
		"datawatch automation approve ab12 9c30ef8e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// A second insert is listed too, with its own approve command.
	c.replyBody = `{"id":"ab12","name":"chain","status":"paused","version":3,"members":[],
"pending_inserts":[
{"id":"9c30ef8e","prd_id":"p-77","reason":"tests found a gap","actor":"agent:7","status":"pending"},
{"id":"d1f0a0be","prd_id":"p-78","reason":"operator asked","actor":"dmz","status":"pending"}]}`
	out = captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "get", "ab12") })
	if err != nil {
		t.Fatalf("get (two inserts): %v", err)
	}
	for _, want := range []string{
		"Pending inserts: 2",
		"d1f0a0be",
		"p-78",
		"operator asked",
		"datawatch automation approve ab12 d1f0a0be",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// The same lines appear in a control verb that prints the Automation.
	c.replyBody = pendingInsertsOne
	out = captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "pause", "ab12", "--expected-version", "3") })
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations/ab12/pause" {
		t.Errorf("proxied %s %s, want POST /api/automations/ab12/pause", c.gotMethod, c.gotPath)
	}
	if body := c.body(t); body["expected_version"] != float64(3) {
		t.Errorf("body expected_version = %v, want 3", body["expected_version"])
	}
	for _, want := range []string{
		"Pending inserts: 1",
		"9c30ef8e",
		"p-77",
		"tests found a gap",
		"datawatch automation approve ab12 9c30ef8e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pause output lacks %q:\n%s", want, out)
		}
	}
}

func TestAutomationCLIGetNoPendingInserts(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"ab12","name":"chain","status":"paused","version":3,"members":[]}`}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "get", "ab12") })
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, "Pending inserts: none") {
		t.Errorf("output lacks 'Pending inserts: none':\n%s", out)
	}
	if strings.Contains(out, "datawatch automation approve") {
		t.Errorf("output suggests an approve command with no pending inserts:\n%s", out)
	}
}
