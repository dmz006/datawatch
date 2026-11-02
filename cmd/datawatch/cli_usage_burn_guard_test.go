package main

import (
	"net/http"
	"testing"
)

func TestBurnGuardCLIStatusAndRun(t *testing.T) {
	seen := usageDaemon(t)
	for _, tc := range []struct {
		arg, method, uri string
	}{
		{"status", http.MethodGet, "/api/usage/burn-guard"},
		{"run", http.MethodPost, "/api/usage/burn-guard/run"},
		{"config", http.MethodGet, "/api/usage/burn-guard/config"},
	} {
		c := newUsageBurnGuardCmd()
		c.SetArgs([]string{tc.arg})
		if err := usageRun(t, c); err != nil {
			t.Fatalf("%s: %v", tc.arg, err)
		}
		calls := seen()
		got := calls[len(calls)-1]
		if got.method != tc.method || got.uri != tc.uri {
			t.Errorf("%s sent %s %s, want %s %s", tc.arg, got.method, got.uri, tc.method, tc.uri)
		}
	}
}

func TestBurnGuardCLIConfigSet(t *testing.T) {
	seen := usageDaemon(t)
	c := newUsageBurnGuardCmd()
	c.SetArgs([]string{"config", "margin_points=3.5", "enabled=false", "ladder=lower_inflight, hold_claude", "interval_minutes=30"})
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	calls := seen()
	if len(calls) != 1 || calls[0].method != http.MethodPut || calls[0].uri != "/api/usage/burn-guard/config" {
		t.Fatalf("calls = %+v", calls)
	}
	b := calls[0].body
	if b["margin_points"] != 3.5 || b["enabled"] != false || b["interval_minutes"] != 30.0 {
		t.Errorf("body = %v", b)
	}
	ladder, _ := b["ladder"].([]any)
	if len(ladder) != 2 || ladder[0] != "lower_inflight" || ladder[1] != "hold_claude" {
		t.Errorf("ladder = %v", b["ladder"])
	}
}

func TestBurnGuardCLIConfigRejectsBadLocalInput(t *testing.T) {
	seen := usageDaemon(t)
	for _, arg := range []string{"nokey", "enabled=maybe", "margin_points=abc", "=3"} {
		c := newUsageBurnGuardCmd()
		c.SetArgs([]string{"config", arg})
		if err := usageRun(t, c); err == nil {
			t.Errorf("config %q: want an error", arg)
		}
	}
	if n := len(seen()); n != 0 {
		t.Errorf("%d requests reached the daemon for invalid input", n)
	}
}
