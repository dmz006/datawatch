// CLI coverage for the Automation LLM settings: set-llm and
// set-verification-llm. Each test serves the daemon side with captureDaemon
// (see cli_autonomous_subscriptions_test.go).
package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const cliSetLLMReply = `{"id":"ab12","name":"fj","status":"running","version":9,"members":[]}`

// setLLMHarness returns helpers that run the automation command against a
// capturing daemon and check the request it saw.
func setLLMHarness(t *testing.T) (c *captureDaemon, run func(args ...string) error, check func(path string, want map[string]any)) {
	t.Helper()
	c = &captureDaemon{replyBody: cliSetLLMReply}
	withCaptureServer(t, c)
	run = func(args ...string) error {
		var err error
		captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), args...) })
		return err
	}
	check = func(path string, want map[string]any) {
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
	return c, run, check
}

func TestAutomationCLISetLLM(t *testing.T) {
	registered := map[string]bool{}
	for _, sub := range newAutomationCmd().Commands() {
		registered[sub.Name()] = true
	}
	if !registered["set-llm"] {
		t.Fatal("automation subcommand set-llm is not registered")
	}
	c, run, check := setLLMHarness(t)

	t.Run("all flags", func(t *testing.T) {
		if err := run("set-llm", "ab12", "--backend", "pi", "--model", "qwen3.8:27b", "--effort", "high",
			"--planning-backend", "claude-code", "--planning-model", "opus"); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_llm", map[string]any{
			"backend": "pi", "model": "qwen3.8:27b", "effort": "high",
			"planning_backend": "claude-code", "planning_model": "opus", "expected_version": float64(9),
		})
	})

	t.Run("only changed flags are sent", func(t *testing.T) {
		if err := run("set-llm", "ab12", "--model", "sonnet"); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_llm", map[string]any{"model": "sonnet", "expected_version": float64(9)})
	})

	t.Run("an empty value clears", func(t *testing.T) {
		if err := run("set-llm", "ab12", "--model", "", "--planning-backend="); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_llm", map[string]any{"model": "", "planning_backend": "", "expected_version": float64(9)})
	})

	t.Run("no setting flag fails", func(t *testing.T) {
		c.gotPath = ""
		err := run("set-llm", "ab12", "--actor", "dmz")
		if err == nil || !strings.Contains(err.Error(), "--backend") {
			t.Errorf("err = %v, want one that names the setting flags", err)
		}
		if c.gotPath != "" {
			t.Errorf("request sent without a setting: path=%q", c.gotPath)
		}
	})

	t.Run("control flags are forwarded", func(t *testing.T) {
		if err := run("set-llm", "ab12", "--effort", "low", "--actor", "dmz", "--reason", "cheaper", "--expected-version", "8"); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_llm", map[string]any{
			"effort": "low", "actor": "dmz", "reason": "cheaper", "expected_version": float64(8),
		})
	})

	t.Run("version conflict is an error", func(t *testing.T) {
		c.replyCode, c.replyBody = http.StatusConflict, "automation ab12 is at version 9, expected 7"
		defer func() { c.replyCode, c.replyBody = 0, cliSetLLMReply }()
		err := run("set-llm", "ab12", "--model", "x", "--expected-version", "7")
		if err == nil || !strings.Contains(err.Error(), "409") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestAutomationCLISetVerificationLLM(t *testing.T) {
	registered := map[string]bool{}
	for _, sub := range newAutomationCmd().Commands() {
		registered[sub.Name()] = true
	}
	if !registered["set-verification-llm"] {
		t.Fatal("automation subcommand set-verification-llm is not registered")
	}
	c, run, check := setLLMHarness(t)

	t.Run("all flags", func(t *testing.T) {
		if err := run("set-verification-llm", "ab12", "--backend", "claude-code", "--model", "haiku", "--effort", "low"); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_verification_llm", map[string]any{
			"backend": "claude-code", "model": "haiku", "effort": "low", "expected_version": float64(9),
		})
	})

	t.Run("only changed flags are sent", func(t *testing.T) {
		if err := run("set-verification-llm", "ab12", "--effort", "medium"); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_verification_llm", map[string]any{"effort": "medium", "expected_version": float64(9)})
	})

	t.Run("an empty value clears", func(t *testing.T) {
		if err := run("set-verification-llm", "ab12", "--backend", ""); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_verification_llm", map[string]any{"backend": "", "expected_version": float64(9)})
	})

	t.Run("no setting flag fails", func(t *testing.T) {
		c.gotPath = ""
		err := run("set-verification-llm", "ab12")
		if err == nil || !strings.Contains(err.Error(), "--backend") {
			t.Errorf("err = %v, want one that names the setting flags", err)
		}
		if c.gotPath != "" {
			t.Errorf("request sent without a setting: path=%q", c.gotPath)
		}
	})

	t.Run("planning flags are not accepted", func(t *testing.T) {
		c.gotPath = ""
		if err := run("set-verification-llm", "ab12", "--planning-model", "x"); err == nil || c.gotPath != "" {
			t.Errorf("planning flag: err = %v, path = %q", err, c.gotPath)
		}
	})

	t.Run("control flags are forwarded", func(t *testing.T) {
		if err := run("set-verification-llm", "ab12", "--model", "haiku", "--expected-version", "8", "--actor", "dmz"); err != nil {
			t.Fatal(err)
		}
		check("/api/automations/ab12/set_verification_llm", map[string]any{
			"model": "haiku", "actor": "dmz", "expected_version": float64(8),
		})
	})
}
