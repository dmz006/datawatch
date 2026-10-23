// CLI coverage for the Automation update, delete and status verbs. Each test
// serves the daemon side with captureDaemon.
package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAutomationCLIUpdate(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"ab12","name":"renamed","status":"draft","version":3,"members":[]}`}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() {
		err = runCmd(t, newAutomationCmd(), "update", "ab12", "--name", "renamed", "--expected-version", "2", "--actor", "dmz",
			"--members", `[{"id":"m1","prd_id":"p-1"}]`)
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if c.gotMethod != http.MethodPut || c.gotPath != "/api/automations/ab12" {
		t.Errorf("proxied %s %s, want PUT /api/automations/ab12", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	if body["name"] != "renamed" || body["actor"] != "dmz" || body["expected_version"] != float64(2) {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["description"]; ok {
		t.Errorf("unset --description was sent: %v", body)
	}
	if ms, _ := body["members"].([]any); len(ms) != 1 {
		t.Errorf("members = %v, want one member", body["members"])
	}
	if !strings.Contains(out, "ab12") {
		t.Errorf("output lacks the Automation id:\n%s", out)
	}
}

func TestAutomationCLIDelete(t *testing.T) {
	c := &captureDaemon{replyCode: http.StatusNoContent}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "delete", "ab12", "--reason", "obsolete") })
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if c.gotMethod != http.MethodDelete || c.gotPath != "/api/automations/ab12" {
		t.Errorf("proxied %s %s, want DELETE /api/automations/ab12", c.gotMethod, c.gotPath)
	}
	if body := c.body(t); body["reason"] != "obsolete" {
		t.Errorf("body = %v", body)
	}
	if !strings.Contains(out, "deleted automation ab12") {
		t.Errorf("output = %q", out)
	}
}

func TestAutomationCLIStatus(t *testing.T) {
	c := &captureDaemon{replyBody: `{"automations":[{"id":"ab12","name":"chain","status":"running","current_member_id":"m1","current_prd_title":"Do it","pending_members":2,"idle_seconds":90,"spent_usd":1.5,"ceiling_display":"$10"}]}`}
	withCaptureServer(t, c)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status") })
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if c.gotMethod != http.MethodGet || c.gotPath != "/api/autonomous/status" {
		t.Errorf("proxied %s %s, want GET /api/autonomous/status", c.gotMethod, c.gotPath)
	}
	for _, want := range []string{"ab12", "chain", "running", "m1 (Do it)", "1m30s", "$1.50 / $10"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	c.replyBody = `{}`
	out = captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "status") })
	if err != nil || !strings.Contains(out, "no Automations") {
		t.Errorf("empty status: err=%v out=%q", err, out)
	}
}
