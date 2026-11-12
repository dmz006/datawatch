// CLI coverage for `datawatch automation set-budget` and the budget block of
// `datawatch automation get`. The daemon side is an in-process test server
// (captureDaemon, see cli_autonomous_subscriptions_test.go).
package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const cliBudgetReply = `{"id":"ab12","name":"nightly","status":"active","version":8,"members":[{"id":"m2","prd_id":"p-2"}],
"budget":{"scope":"automation","ceiling_type":"dollar","ceiling_usd":40,"spent_usd":12.4,"remaining_usd":27.6,
"window":"week","window_start":"2026-10-19T00:00:00Z","window_end":"2026-10-26T00:00:00Z","on_breach":"pause","pacing":"none",
"ceiling_reached":false,"reset_at":"2026-10-26T00:00:00Z","reset_known":true,"reset_source":"window",
"allocation_estimate":{"allocation_pct":25,"estimated":true,"allocation_usd":40,"plan_tier":"max_5x"},
"peers":[{"peer":"ci-box","spend_usd":3.2,"counted":true}]}}`

func TestAutomationSetBudgetCLI(t *testing.T) {
	var registered bool
	for _, sub := range newAutomationCmd().Commands() {
		registered = registered || sub.Name() == "set-budget"
	}
	if !registered {
		t.Fatal("automation subcommand set-budget is not registered")
	}

	c := &captureDaemon{replyBody: cliBudgetReply}
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
	check := func(t *testing.T, want map[string]any) {
		t.Helper()
		if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations/ab12/set_budget" {
			t.Errorf("proxied %s %s, want POST /api/automations/ab12/set_budget", c.gotMethod, c.gotPath)
		}
		body := c.body(t)
		if len(body) != len(want) {
			t.Errorf("body = %v, want %v", body, want)
		}
		for k, v := range want {
			if !reflect.DeepEqual(body[k], v) {
				t.Errorf("body[%q] = %v (%T), want %v (%T)", k, body[k], body[k], v, v)
			}
		}
	}

	t.Run("sets a policy and prints the budget block", func(t *testing.T) {
		out := run(t, "set-budget", "ab12", "--expected-version", "7", "--ceiling-type", "dollar", "--ceiling-value", "40",
			"--window", "week", "--pacing", "even", "--on-breach", "pause", "--backend", "claude-code",
			"--allocation-estimate", "--peer-window", "ci-box, laptop", "--reset-time", "06:00",
			"--reset-weekday", "monday", "--reset-zone", "America/New_York", "--reason", "weekly cap")
		check(t, map[string]any{
			"expected_version": float64(7), "ceiling_type": "dollar", "ceiling_value": float64(40), "window": "week",
			"pacing": "even", "on_breach": "pause", "backend": "claude-code", "allocation_estimate": true,
			"peer_window": []any{"ci-box", "laptop"}, "reset_time": "06:00", "reset_weekday": "monday",
			"reset_zone": "America/New_York", "reason": "weekly cap",
		})
		for _, want := range []string{
			"Version:  8",
			"budget (automation)",
			"spent: $12.40 of $40.00 ceiling (week window)",
			"on breach: pauses and resumes by itself at the reset",
			"allocation (estimated): ~25% of $40.00 (max_5x)",
			"peer ci-box: counted, $3.20",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("member override and its estimated cost", func(t *testing.T) {
		run(t, "set-budget", "ab12", "--member", "m2", "--ceiling-type", "dollar", "--ceiling-value", "5.5",
			"--window", "automation", "--on-breach", "block", "--estimated-cost-usd", "2.5")
		check(t, map[string]any{
			"member_id": "m2", "ceiling_type": "dollar", "ceiling_value": 5.5, "window": "automation",
			"on_breach": "block", "estimated_cost_usd": 2.5, "expected_version": float64(8),
		})
	})

	t.Run("estimated cost alone, zero removes it", func(t *testing.T) {
		run(t, "set-budget", "ab12", "--member", "m2", "--estimated-cost-usd", "0")
		check(t, map[string]any{"member_id": "m2", "estimated_cost_usd": float64(0), "expected_version": float64(8)})
	})

	t.Run("month window anchors", func(t *testing.T) {
		run(t, "set-budget", "ab12", "--ceiling-type", "dollar", "--ceiling-value", "100", "--window", "month", "--reset-day", "15")
		check(t, map[string]any{"ceiling_type": "dollar", "ceiling_value": float64(100), "window": "month", "reset_day": float64(15), "expected_version": float64(8)})
	})

	t.Run("clear", func(t *testing.T) {
		run(t, "set-budget", "ab12", "--clear", "--expected-version", "8")
		check(t, map[string]any{"clear": true, "expected_version": float64(8)})
		run(t, "set-budget", "ab12", "--member", "m2", "--clear")
		check(t, map[string]any{"clear": true, "member_id": "m2", "expected_version": float64(8)})
	})

	t.Run("a refusal carries the daemon message", func(t *testing.T) {
		c.replyCode, c.replyBody = http.StatusConflict, "409: version mismatch: expected version 7, current version is 9"
		defer func() { c.replyCode, c.replyBody = 0, cliBudgetReply }()
		err := runCmd(t, newAutomationCmd(), "set-budget", "ab12", "--expected-version", "7", "--clear")
		if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "current version is 9") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("get prints the budget block when present", func(t *testing.T) {
		c.replyBody = cliBudgetReply
		out := run(t, "get", "ab12")
		if c.gotMethod != http.MethodGet || c.gotPath != "/api/automations/ab12" {
			t.Errorf("proxied %s %s", c.gotMethod, c.gotPath)
		}
		if !strings.Contains(out, "budget (automation)") || !strings.Contains(out, "reset: 2026-10-26T00:00:00Z (window)") {
			t.Errorf("get output lacks the budget block:\n%s", out)
		}
		c.replyBody = `{"id":"ab12","name":"nightly","status":"active","version":8,"members":[]}`
		if out := run(t, "get", "ab12"); strings.Contains(out, "budget") {
			t.Errorf("no budget block expected without a budget view:\n%s", out)
		}
	})
}

func TestAutomationSetBudgetCLIRejectsBadFlags(t *testing.T) {
	c := &captureDaemon{replyBody: cliBudgetReply}
	withCaptureServer(t, c)

	good := []string{"--ceiling-type", "dollar", "--ceiling-value", "40", "--window", "week"}
	with := func(extra ...string) []string { return append(append([]string{}, good...), extra...) }
	cases := []struct {
		name string
		args []string
		flag string // the flag the message must name
	}{
		{"nothing to set", nil, "--clear"},
		{"clear with policy flags", []string{"--clear", "--ceiling-type", "dollar", "--on-breach", "block"}, "--ceiling-type"},
		{"clear with estimated cost", []string{"--clear", "--member", "m2", "--estimated-cost-usd", "1"}, "--estimated-cost-usd"},
		{"estimated cost needs a member", []string{"--estimated-cost-usd", "1"}, "--member"},
		{"negative estimated cost", []string{"--member", "m2", "--estimated-cost-usd", "-1"}, "--estimated-cost-usd"},
		{"missing ceiling type", []string{"--ceiling-value", "40", "--window", "week"}, "--ceiling-type"},
		{"bad ceiling type", []string{"--ceiling-type", "euro", "--ceiling-value", "40", "--window", "week"}, "--ceiling-type"},
		{"missing ceiling value", []string{"--ceiling-type", "dollar", "--window", "week"}, "--ceiling-value"},
		{"zero ceiling value", []string{"--ceiling-type", "dollar", "--ceiling-value", "0", "--window", "week"}, "--ceiling-value"},
		{"missing window", []string{"--ceiling-type", "dollar", "--ceiling-value", "40"}, "--window"},
		{"bad window", []string{"--ceiling-type", "dollar", "--ceiling-value", "40", "--window", "year"}, "--window"},
		{"bad pacing", with("--pacing", "fast"), "--pacing"},
		{"bad on-breach", with("--on-breach", "explode"), "--on-breach"},
		{"reset-time on the lifetime window", []string{"--ceiling-type", "dollar", "--ceiling-value", "40", "--window", "automation", "--reset-time", "06:00"}, "--reset-time"},
		{"unparsable reset-time", with("--reset-time", "6pm"), "--reset-time"},
		{"unknown reset-zone", with("--reset-zone", "Mars/Olympus"), "--reset-zone"},
		{"reset-weekday on a day window", []string{"--ceiling-type", "dollar", "--ceiling-value", "40", "--window", "day", "--reset-weekday", "monday"}, "--reset-weekday"},
		{"unknown reset-weekday", with("--reset-weekday", "someday"), "--reset-weekday"},
		{"reset-day on a week window", with("--reset-day", "5"), "--reset-day"},
		{"reset-day out of range", []string{"--ceiling-type", "dollar", "--ceiling-value", "40", "--window", "month", "--reset-day", "31"}, "--reset-day"},
		{"empty peer", with("--peer-window", "ci-box,,laptop"), "--peer-window"},
		{"duplicate peer", with("--peer-window", "ci-box,ci-box"), "--peer-window"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.gotPath = ""
			err := runCmd(t, newAutomationCmd(), append([]string{"set-budget", "ab12"}, tc.args...)...)
			if err == nil {
				t.Fatalf("accepted: %v", tc.args)
			}
			if !strings.Contains(err.Error(), tc.flag) {
				t.Errorf("err = %q, want one that names %s", err, tc.flag)
			}
			if c.gotPath != "" {
				t.Errorf("a request was sent (%s) for invalid flags", c.gotPath)
			}
		})
	}

	t.Run("the id is required", func(t *testing.T) {
		if err := runCmd(t, newAutomationCmd(), "set-budget", "--clear"); err == nil {
			t.Error("accepted without an id")
		}
	})
}
