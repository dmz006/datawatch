// CLI coverage for `datawatch automation set-decision-assistant`. The daemon
// side is a captureDaemon (see cli_autonomous_subscriptions_test.go).
package main

import (
	"strings"
	"testing"
)

func TestAutomationCLIDecisionAssistant(t *testing.T) {
	registered := false
	for _, sub := range newAutomationCmd().Commands() {
		registered = registered || sub.Name() == "set-decision-assistant"
	}
	if !registered {
		t.Fatal("automation subcommand set-decision-assistant is not registered")
	}
	_, run, check := setLLMHarness(t)
	path := "/api/automations/ab12/set_decision_assistant"

	t.Run("set", func(t *testing.T) {
		if err := run("set-decision-assistant", "ab12", "--backend", "pi-prompt", "--model", "qwen3.8:27b"); err != nil {
			t.Fatal(err)
		}
		check(path, map[string]any{"backend": "pi-prompt", "model": "qwen3.8:27b", "expected_version": float64(9)})
	})
	t.Run("full", func(t *testing.T) {
		if err := run("set-decision-assistant", "ab12", "--backend", "pi-prompt", "--model", "m", "--mode", "live",
			"--hooks", "empty_edge,budget_breach", "--threshold", "empty_edge=0.8", "--threshold", "budget_breach=0.99",
			"--member", "m1", "--actor", "dmz", "--reason", "pilot", "--expected-version", "8"); err != nil {
			t.Fatal(err)
		}
		check(path, map[string]any{
			"backend": "pi-prompt", "model": "m", "mode": "live",
			"hooks":      []any{"empty_edge", "budget_breach"},
			"thresholds": map[string]any{"empty_edge": 0.8, "budget_breach": 0.99},
			"member_id":  "m1", "actor": "dmz", "reason": "pilot", "expected_version": float64(8),
		})
	})
	t.Run("clear", func(t *testing.T) {
		if err := run("set-decision-assistant", "ab12", "--clear"); err != nil {
			t.Fatal(err)
		}
		check(path, map[string]any{"clear": true, "expected_version": float64(9)})
	})
	t.Run("empty backend clears", func(t *testing.T) {
		if err := run("set-decision-assistant", "ab12", "--backend", ""); err != nil {
			t.Fatal(err)
		}
		check(path, map[string]any{"backend": "", "expected_version": float64(9)})
	})
}

func TestAutomationCLIDecisionAssistantRefusals(t *testing.T) {
	c, run, _ := setLLMHarness(t)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"nothing named":     {[]string{"set-decision-assistant", "ab12"}, "--clear"},
		"backend only":      {[]string{"set-decision-assistant", "ab12", "--backend", "pi-prompt"}, "required together"},
		"model only":        {[]string{"set-decision-assistant", "ab12", "--model", "m"}, "required together"},
		"clear with fields": {[]string{"set-decision-assistant", "ab12", "--clear", "--backend", "b", "--model", "m"}, "cannot be combined"},
		"bad mode":          {[]string{"set-decision-assistant", "ab12", "--backend", "b", "--model", "m", "--mode", "auto"}, "shadow or live"},
		"bad threshold":     {[]string{"set-decision-assistant", "ab12", "--backend", "b", "--model", "m", "--threshold", "empty_edge"}, "<hook>=<value>"},
		"threshold number":  {[]string{"set-decision-assistant", "ab12", "--backend", "b", "--model", "m", "--threshold", "empty_edge=high"}, "not a number"},
	} {
		c.gotPath = ""
		err := run(tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, tc.want)
		}
		if c.gotPath != "" {
			t.Errorf("%s reached the daemon at %s", name, c.gotPath)
		}
	}
}

func TestAutomationCLIDecisionAssistantText(t *testing.T) {
	if got := decisionAssistantText(nil); got != "off" {
		t.Errorf("nil = %q, want off", got)
	}
}
