// Sprint Sx — smoke tests for the new CLI subcommands.
// These verify the commands are registered with the right names +
// flags. Functional behaviour is exercised by the REST handler tests
// in internal/server (since the CLI is a thin REST proxy).

package main

import (
	"strings"
	"testing"
)

func TestSx_CLI_Ask_Registered(t *testing.T) {
	c := newAskCmd()
	if c.Name() != "ask" {
		t.Errorf("name=%q want ask", c.Name())
	}
	if !strings.Contains(c.Short, "BL34") {
		t.Errorf("short missing BL34 ref: %q", c.Short)
	}
	if c.Flag("backend") == nil {
		t.Errorf("--backend flag missing")
	}
}

func TestSx_CLI_ProjectSummary_Registered(t *testing.T) {
	c := newProjectSummaryCmd()
	if c.Name() != "project-summary" {
		t.Errorf("name=%q", c.Name())
	}
	if c.Flag("dir") == nil {
		t.Errorf("--dir flag missing")
	}
}

func TestSx_CLI_Template_HasSubs(t *testing.T) {
	c := newTemplateCmd()
	wantSubs := map[string]bool{"list": false, "get": false, "upsert": false, "delete": false}
	for _, sub := range c.Commands() {
		wantSubs[sub.Name()] = true
	}
	for sub, ok := range wantSubs {
		if !ok {
			t.Errorf("template missing subcommand: %s", sub)
		}
	}
}

func TestSx_CLI_Projects_HasSubs(t *testing.T) {
	c := newProjectsCmd()
	wantSubs := map[string]bool{"list": false, "get": false, "upsert": false, "delete": false}
	for _, sub := range c.Commands() {
		wantSubs[sub.Name()] = true
	}
	for sub, ok := range wantSubs {
		if !ok {
			t.Errorf("projects missing subcommand: %s", sub)
		}
	}
}

func TestSx_CLI_Rollback_HasForceFlag(t *testing.T) {
	c := newRollbackCmd()
	if c.Flag("force") == nil {
		t.Errorf("--force flag missing")
	}
}

func TestSx_CLI_Cooldown_HasSubs(t *testing.T) {
	c := newCooldownCmd()
	wantSubs := map[string]bool{"status": false, "set": false, "clear": false}
	for _, sub := range c.Commands() {
		wantSubs[sub.Name()] = true
	}
	for sub, ok := range wantSubs {
		if !ok {
			t.Errorf("cooldown missing subcommand: %s", sub)
		}
	}
}

func TestSx_CLI_Stale_HasSecondsFlag(t *testing.T) {
	c := newStaleCmd()
	if c.Flag("seconds") == nil {
		t.Errorf("--seconds flag missing")
	}
}

func TestSx_CLI_Cost_HasSubs(t *testing.T) {
	c := newCostCmd()
	wantSubs := map[string]bool{"summary": false, "usage": false, "rates": false}
	for _, sub := range c.Commands() {
		wantSubs[sub.Name()] = true
	}
	for sub, ok := range wantSubs {
		if !ok {
			t.Errorf("cost missing subcommand: %s", sub)
		}
	}
}

func TestSx_CLI_Audit_HasFilters(t *testing.T) {
	c := newAuditCmd()
	for _, f := range []string{"actor", "action", "session-id", "since", "until", "limit"} {
		if c.Flag(f) == nil {
			t.Errorf("--%s flag missing", f)
		}
	}
}

func TestSx_CLI_JoinArgs(t *testing.T) {
	if got := joinArgs([]string{"a", "b", "c"}); got != "a b c" {
		t.Errorf("got %q want 'a b c'", got)
	}
	if got := joinArgs([]string{"single"}); got != "single" {
		t.Errorf("single arg: got %q", got)
	}
}
