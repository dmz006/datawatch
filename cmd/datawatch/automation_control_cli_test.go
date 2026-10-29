// CLI coverage for the Automation operator controls: pause, resume, cancel,
// skip, insert, remove, edges and approve. Each test serves the daemon side
// with captureDaemon (see cli_autonomous_subscriptions_test.go).
package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const cliControlReply = `{"id":"ab12","name":"fj","status":"paused","version":8,"members":[{"id":"m2","prd_id":"p-2"}]}`

func TestAutomationCLIControl(t *testing.T) {
	// All eight controls are registered.
	registered := map[string]bool{}
	for _, sub := range newAutomationCmd().Commands() {
		registered[sub.Name()] = true
	}
	for _, name := range []string{"pause", "resume", "cancel", "skip", "insert", "remove", "edges", "approve"} {
		if !registered[name] {
			t.Errorf("automation subcommand %q is not registered", name)
		}
	}

	c := &captureDaemon{replyBody: cliControlReply}
	withCaptureServer(t, c)

	run := func(t *testing.T, args ...string) string {
		t.Helper()
		var err error
		out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), args...) })
		if err != nil {
			t.Fatalf("automation %s: %v", strings.Join(args, " "), err)
		}
		return out
	}
	check := func(t *testing.T, path string, want map[string]any) {
		t.Helper()
		if c.gotMethod != http.MethodPost || c.gotPath != path {
			t.Errorf("proxied %s %s, want POST %s", c.gotMethod, c.gotPath, path)
		}
		body := c.body(t)
		if len(body) != len(want) {
			t.Errorf("body = %v, want %v", body, want)
		}
		for k, v := range want {
			if !reflect.DeepEqual(body[k], v) {
				t.Errorf("body[%q] = %v, want %v (body %v)", k, body[k], v, body)
			}
		}
	}

	t.Run("cancel needs --cancel-in-flight", func(t *testing.T) {
		c.gotPath = ""
		err := runCmd(t, newAutomationCmd(), "cancel", "ab12")
		if err == nil || !strings.Contains(err.Error(), "--cancel-in-flight") {
			t.Errorf("err = %v, want one that names --cancel-in-flight", err)
		}
		if err := runCmd(t, newAutomationCmd(), "cancel", "ab12", "--cancel-in-flight", "maybe"); err == nil {
			t.Error("a non-boolean --cancel-in-flight was accepted")
		}
		if c.gotPath != "" {
			t.Errorf("request sent without a valid choice: path=%q", c.gotPath)
		}
	})

	t.Run("cancel posts the choice", func(t *testing.T) {
		out := run(t, "cancel", "ab12", "--cancel-in-flight", "true", "--actor", "dmz", "--expected-version", "7")
		check(t, "/api/automations/ab12/cancel", map[string]any{"cancel_in_flight": true, "actor": "dmz", "expected_version": float64(7)})
		for _, want := range []string{"ID:       ab12", "Version:  8"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
		run(t, "cancel", "ab12", "--cancel-in-flight=false")
		check(t, "/api/automations/ab12/cancel", map[string]any{"cancel_in_flight": false, "expected_version": float64(8)})
	})

	t.Run("pause and resume", func(t *testing.T) {
		run(t, "pause", "ab12", "--reason", "hold for review")
		check(t, "/api/automations/ab12/pause", map[string]any{"reason": "hold for review", "expected_version": float64(8)})
		run(t, "resume", "ab12")
		check(t, "/api/automations/ab12/resume", map[string]any{"expected_version": float64(8)})
	})

	t.Run("skip", func(t *testing.T) {
		run(t, "skip", "ab12", "m2")
		check(t, "/api/automations/ab12/skip_member", map[string]any{"member_id": "m2", "edge": "on_complete", "expected_version": float64(8)})
		run(t, "skip", "ab12", "m2", "--edge", "on_failed")
		check(t, "/api/automations/ab12/skip_member", map[string]any{"member_id": "m2", "edge": "on_failed", "expected_version": float64(8)})
		c.gotPath = ""
		if err := runCmd(t, newAutomationCmd(), "skip", "ab12", "m2", "--edge", "sideways"); err == nil || c.gotPath != "" {
			t.Errorf("bad --edge: err = %v, path = %q", err, c.gotPath)
		}
	})

	t.Run("insert", func(t *testing.T) {
		run(t, "insert", "ab12", "prd-9", "--reason", "fix the flaky step", "--urgent", "--after", "m2")
		check(t, "/api/automations/ab12/insert_member", map[string]any{
			"member": map[string]any{"prd_id": "prd-9"}, "urgent": true, "after": "m2", "reason": "fix the flaky step", "expected_version": float64(8),
		})
		c.gotPath = ""
		if err := runCmd(t, newAutomationCmd(), "insert", "ab12", "prd-9"); err == nil || c.gotPath != "" {
			t.Errorf("insert without --reason: err = %v, path = %q", err, c.gotPath)
		}
	})

	t.Run("remove", func(t *testing.T) {
		run(t, "remove", "ab12", "m3")
		check(t, "/api/automations/ab12/remove_member", map[string]any{"member_id": "m3", "expected_version": float64(8)})
	})

	t.Run("edges", func(t *testing.T) {
		run(t, "edges", "ab12", "m2", "--on-complete", "m4", "--on-failed", "m5")
		check(t, "/api/automations/ab12/reorder_edges", map[string]any{
			"member_id": "m2", "on_complete": "m4", "on_blocked": "", "on_failed": "m5", "expected_version": float64(8),
		})
		c.gotPath = ""
		if err := runCmd(t, newAutomationCmd(), "edges", "ab12", "m2"); err == nil || c.gotPath != "" {
			t.Errorf("edges with no edge flag: err = %v, path = %q", err, c.gotPath)
		}
	})

	t.Run("approve", func(t *testing.T) {
		run(t, "approve", "ab12", "m6")
		check(t, "/api/automations/ab12/approve_insert", map[string]any{"member_id": "m6", "expected_version": float64(8)})
	})

	t.Run("version conflict is an error", func(t *testing.T) {
		c.replyCode, c.replyBody = http.StatusConflict, "automation ab12 is at version 9, expected 7"
		defer func() { c.replyCode, c.replyBody = 0, cliControlReply }()
		err := runCmd(t, newAutomationCmd(), "pause", "ab12", "--expected-version", "7")
		if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "is at version 9") {
			t.Errorf("err = %v", err)
		}
	})
}
