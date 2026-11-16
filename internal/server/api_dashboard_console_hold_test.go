// Tests for how the operator console counts Automata the operator put on hold.

package server

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// consoleHoldSpy is the console fixture's manager with a loop status that
// reports a number of held Automata.
type consoleHoldSpy struct {
	*consoleSpy
	status any
}

func (c *consoleHoldSpy) Status() any { return c.status }

func TestHoldDashboardConsoleHeldNotStuck(t *testing.T) {
	cases := []struct {
		name       string
		status     any
		wantHeld   string
		wantNoHeld bool
	}{
		{"value status", autonomous.LoopStatus{HeldPRDs: 2}, ", 2 held (not counted as stuck)", false},
		{"pointer status", &autonomous.LoopStatus{HeldPRDs: 1}, ", 1 held (not counted as stuck)", false},
		{"no holds", autonomous.LoopStatus{}, "", true},
		{"no loop status", nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := consoleFixtureServer(t)
			spy := s.autonomousMgr.(*consoleSpy)
			// A merge-pending record is neither failed nor blocked, so it is
			// not stuck either.
			spy.triage = append(spy.triage, &autonomous.TriageRecord{PRDID: "p3", Status: autonomous.TriageStatusMergePending})
			s.autonomousMgr = &consoleHoldSpy{consoleSpy: spy, status: tc.status}

			_, resp := getConsole(t, s)
			sec := resp.HeldBlocked
			if !sec.Available {
				t.Fatalf("held_blocked unavailable: %q", sec.Reason)
			}
			if sec.Count != 2 {
				t.Errorf("count = %d, want 2: held Automata and merge-pending records are not stuck", sec.Count)
			}
			if !strings.Contains(sec.Summary, "2 automaton(s) stuck: 1 failed, 1 blocked") {
				t.Errorf("summary %q does not carry the failed/blocked split", sec.Summary)
			}
			if tc.wantNoHeld {
				if strings.Contains(sec.Summary, "held (not counted as stuck)") {
					t.Errorf("summary %q names holds when there are none", sec.Summary)
				}
			} else if !strings.Contains(sec.Summary, tc.wantHeld) {
				t.Errorf("summary %q lacks %q", sec.Summary, tc.wantHeld)
			}
			if !strings.Contains(sec.Summary, "1 uncommitted-work hold") {
				t.Errorf("summary %q lost the uncommitted-work hold", sec.Summary)
			}
			if sec.Link != consoleAnchorHeldBlocked {
				t.Errorf("link = %q, want the unchanged anchor %q", sec.Link, consoleAnchorHeldBlocked)
			}
		})
	}
}
