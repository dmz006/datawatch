// CLI coverage for `automation return-to-draft <id>`: the thin sibling of
// activate that returns a blocked Automation (nothing started) to draft.
// Served with captureDaemon (see cli_autonomous_subscriptions_test.go).
package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAutomationCLIReturnToDraft(t *testing.T) {
	registered := map[string]bool{}
	for _, sub := range newAutomationCmd().Commands() {
		registered[sub.Name()] = true
	}
	if !registered["return-to-draft"] {
		t.Fatal("automation subcommand \"return-to-draft\" is not registered")
	}

	c := &captureDaemon{replyBody: cliControlReply}
	withCaptureServer(t, c)

	run := func(t *testing.T, args ...string) (string, error) {
		t.Helper()
		var err error
		out := captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), args...) })
		return out, err
	}

	t.Run("explicit --expected-version posts directly", func(t *testing.T) {
		c.replyCode, c.gotPath = 0, ""
		out, err := run(t, "return-to-draft", "ab12", "--expected-version", "7", "--actor", "dmz")
		if err != nil {
			t.Fatalf("return-to-draft: %v", err)
		}
		if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations/ab12/return_to_draft" {
			t.Errorf("proxied %s %s, want POST /api/automations/ab12/return_to_draft", c.gotMethod, c.gotPath)
		}
		body := c.body(t)
		if body["expected_version"] != float64(7) || body["actor"] != "dmz" {
			t.Errorf("body = %v", body)
		}
		for _, want := range []string{"ID:       ab12", "Version:  8"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("omitted --expected-version reads the current version first", func(t *testing.T) {
		c.replyCode, c.gotPath = 0, ""
		if _, err := run(t, "return-to-draft", "ab12"); err != nil {
			t.Fatalf("return-to-draft: %v", err)
		}
		if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations/ab12/return_to_draft" {
			t.Errorf("proxied %s %s", c.gotMethod, c.gotPath)
		}
		body := c.body(t)
		if body["expected_version"] != float64(8) {
			t.Errorf("body expected_version = %v, want 8 read from the daemon (body %v)", body["expected_version"], body)
		}
		if _, ok := body["actor"]; ok {
			t.Errorf("empty actor should not be sent: %v", body)
		}
	})

	t.Run("a 409 refusal is surfaced", func(t *testing.T) {
		c.replyCode = http.StatusConflict
		c.replyBody = "automation ab12 is active; only a blocked automation with nothing started can go back to draft"
		c.gotPath = ""
		_, err := run(t, "return-to-draft", "ab12", "--expected-version", "7")
		if err == nil || !strings.Contains(err.Error(), "409") {
			t.Errorf("err = %v, want the 409 refusal", err)
		}
	})

	t.Run("an id is required", func(t *testing.T) {
		c.gotPath = ""
		if err := runCmd(t, newAutomationCmd(), "return-to-draft"); err == nil {
			t.Error("return-to-draft without an id was accepted")
		}
	})
}
