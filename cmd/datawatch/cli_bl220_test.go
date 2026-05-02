// BL220 — smoke tests for analytics and proxy CLI subcommands.

package main

import (
	"testing"
)

func TestBL220_CLI_Analytics_Registered(t *testing.T) {
	c := newAnalyticsCmd()
	if c.Name() != "analytics" {
		t.Errorf("name=%q want analytics", c.Name())
	}
	if c.Flag("range") == nil {
		t.Error("--range flag missing")
	}
}

func TestBL220_CLI_Proxy_HasSubs(t *testing.T) {
	c := newProxyCmd()
	if c.Name() != "proxy" {
		t.Errorf("name=%q want proxy", c.Name())
	}
	want := map[string]bool{"config-get": false, "config-set": false}
	for _, sub := range c.Commands() {
		want[sub.Name()] = true
	}
	for sub, ok := range want {
		if !ok {
			t.Errorf("proxy missing subcommand: %s", sub)
		}
	}
}

func TestBL220_CLI_ProxyConfigSet_HasFlags(t *testing.T) {
	c := newProxyConfigSetCmd()
	for _, f := range []string{
		"enabled",
		"health-interval",
		"request-timeout",
		"offline-queue-size",
		"circuit-breaker-threshold",
		"circuit-breaker-reset",
	} {
		if c.Flag(f) == nil {
			t.Errorf("--%s flag missing", f)
		}
	}
}
