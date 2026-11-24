package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
)

// TestDashboardUsageShowsThePaceHold: the dashboard console's usage line says what the pace
// policy holds (a real hold) or would hold (shadow), so an enforcement decision is visible
// there as well as on the status.
func TestDashboardUsageShowsThePaceHold(t *testing.T) {
	s := consoleFixtureServer(t)
	g := s.burnGuard
	set := func(mode string, real, virtual bool) {
		g.mu.Lock()
		g.pace = map[string]*paceState{"claude-code": {real: real, virtual: virtual, rule: burnguard.PaceRuleWindow, since: time.Now(),
			last: burnguard.PaceDecision{Rule: burnguard.PaceRuleWindow, Action: burnguard.PaceActionHold}}}
		g.paceOrder = []string{"claude-code"}
		g.mu.Unlock()
		cfg := config.DefaultBurnGuard()
		cfg.PaceLineMode = mode
		g.in.Config = func() config.BurnGuardConfig { return cfg }
	}

	set("on", true, true)
	_, text, err := s.buildUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pace policy on", "claude-code new work held", "window_pause_line"} {
		if !strings.Contains(text, want) {
			t.Errorf("usage line lacks %q: %s", want, text)
		}
	}

	set("shadow", false, true)
	_, text, err = s.buildUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "pace policy shadow") || !strings.Contains(text, "would be held") || strings.Contains(text, "new work held") {
		t.Errorf("shadow usage line = %s", text)
	}
}
