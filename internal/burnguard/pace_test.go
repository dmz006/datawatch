package burnguard

import (
	"strings"
	"testing"
	"time"
)

// paceClock is the fake clock every pace test reads: a Friday evening in UTC. The window
// that holds it runs 17:00 to 22:00.
var paceClock = time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC)

func paceSettings(mode string) PaceSettings {
	return PaceSettings{
		Mode: mode, WeekTolerance: 2, PointsToWeekFactor: 1, WindowPercentPerPoint: 4,
		WindowStopPercent: 85, ReadingFreshnessMinutes: 60,
	}
}

// paceInput is a week reading at 50% against a pace line of 60 (well under it) and a window
// reading at 10% taken an hour ago in the current window.
func paceInput(mode string) PaceInput {
	return PaceInput{
		Entry: "claude-code", Settings: paceSettings(mode), Now: paceClock,
		WeekKnown: true, WeekReadAt: paceClock.Add(-30 * time.Minute), WeekPercent: 50, LineNow: 60,
		WindowKnown: true, WindowReadAt: paceClock.Add(-time.Hour), WindowPercent: 10,
	}
}

func TestEvaluatePaceRungs(t *testing.T) {
	cases := []struct {
		name     string
		mod      func(*PaceInput)
		wantHold bool
		wantRule string
		wantNote string
	}{
		{"under both lines", func(in *PaceInput) {}, false, "", ""},
		{"week exactly at line plus tolerance is not over", func(in *PaceInput) { in.WeekPercent = 62 }, false, "", ""},
		{"week over line plus tolerance holds", func(in *PaceInput) { in.WeekPercent = 62.1 }, true, PaceRuleWeek, "62.1"},
		{"week estimate adds the points since the reading times the factor", func(in *PaceInput) {
			in.WeekPercent, in.WeekPointsSince, in.Settings.PointsToWeekFactor = 60, 2, 1.5
		}, true, PaceRuleWeek, "63.0"}, // 60 + 2*1.5 = 63 > 62
		{"tolerance widens the allowance", func(in *PaceInput) { in.WeekPercent = 64; in.Settings.WeekTolerance = 5 }, false, "", ""},
		{"window just under the pause line", func(in *PaceInput) { in.WindowPercent = 84.9 }, false, "", ""},
		{"window at the pause line holds", func(in *PaceInput) { in.WindowPercent = 85 }, true, PaceRuleWindow, "85.0"},
		{"window estimate adds points since its reading at the percent per point", func(in *PaceInput) {
			in.WindowPercent, in.WindowPointsSince = 70, 4 // 70 + 4*4 = 86
		}, true, PaceRuleWindow, "86.0"},
		{"window reading from an earlier window counts only the burn since the window began", func(in *PaceInput) {
			in.WindowReadAt = paceClock.Add(-6 * time.Hour) // before the 17:00 start
			in.WindowPercent, in.WindowPointsSinceStart = 99, 1
		}, false, "", ""},
		{"window reading from an earlier window plus a heavy burn since the window began", func(in *PaceInput) {
			in.WindowReadAt = paceClock.Add(-6 * time.Hour)
			in.WindowPercent, in.WindowPointsSinceStart = 99, 22 // 22*4 = 88
		}, true, PaceRuleWindow, "88.0"},
		{"week and window both over: the window rule is named first", func(in *PaceInput) {
			in.WeekPercent, in.WindowPercent = 90, 90
		}, true, PaceRuleWindow, ""},
		{"no weekly reading yet: the week side holds nothing", func(in *PaceInput) { in.WeekKnown = false; in.WeekPercent = 99 }, false, "", ""},
		{"no window reading: the window side holds nothing", func(in *PaceInput) { in.WindowKnown = false; in.WindowPercent = 99 }, false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := paceInput(PaceModeOn)
			c.mod(&in)
			d := EvaluatePace(in)
			if d.Hold != c.wantHold || d.Rule != c.wantRule {
				t.Fatalf("hold=%v rule=%q, want hold=%v rule=%q (decision %+v)", d.Hold, d.Rule, c.wantHold, c.wantRule, d)
			}
			if c.wantNote != "" && !strings.Contains(d.Reason, c.wantNote) {
				t.Fatalf("reason %q does not carry the figure %q", d.Reason, c.wantNote)
			}
			if d.Entry != "claude-code" || !d.At.Equal(paceClock) || d.Mode != PaceModeOn {
				t.Fatalf("evidence header wrong: %+v", d)
			}
		})
	}
}

// TestEvaluatePaceEvidence: a decision carries the numbers it was made on.
func TestEvaluatePaceEvidence(t *testing.T) {
	in := paceInput(PaceModeOn)
	in.WeekPercent, in.WeekPointsSince = 61, 1
	in.WindowPercent, in.WindowPointsSince = 80, 1
	d := EvaluatePace(in)
	if d.WeekEstimated != 62 || d.WeekLine != 60 || d.WeekTolerance != 2 || d.WeekPercent != 61 {
		t.Fatalf("week evidence = %+v", d)
	}
	if d.WindowEstimated != 84 || d.WindowStopPercent != 85 {
		t.Fatalf("window evidence = %+v", d)
	}
	wantStart := time.Date(2026, 10, 30, 17, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 30, 22, 0, 0, 0, time.UTC)
	if !d.WindowStart.Equal(wantStart) || !d.WindowEnd.Equal(wantEnd) {
		t.Fatalf("window = %v to %v, want %v to %v", d.WindowStart, d.WindowEnd, wantStart, wantEnd)
	}
	if d.WeekReadAt.IsZero() || d.WindowReadAt.IsZero() || d.ReadingAgeMinutes != 30 {
		t.Fatalf("reading evidence = %+v", d)
	}
}

func TestEvaluatePaceStaleReadingIsFlaggedNotIgnored(t *testing.T) {
	in := paceInput(PaceModeOn)
	in.WeekReadAt = paceClock.Add(-3 * time.Hour)
	in.WeekPercent = 70
	d := EvaluatePace(in)
	if !d.Stale || !d.Hold {
		t.Fatalf("a stale reading is still decided on and flagged: %+v", d)
	}
	if !strings.Contains(d.Reason, "fresh reading") {
		t.Fatalf("reason %q does not ask for a fresh reading", d.Reason)
	}
}

// TestEvaluatePaceModes: off decides nothing, shadow records what it would do, on does it.
func TestEvaluatePaceModes(t *testing.T) {
	over := func(in *PaceInput) { in.WeekPercent = 70 }
	cases := []struct {
		name       string
		mode       string
		held       bool
		mod        func(*PaceInput)
		wantAction string
		wantHold   bool // the state the caller keeps afterwards
	}{
		{"on, over, not held: hold", PaceModeOn, false, over, PaceActionHold, true},
		{"on, over, held: stays held", PaceModeOn, true, over, PaceActionNone, true},
		{"on, under, held: release", PaceModeOn, true, func(*PaceInput) {}, PaceActionRelease, false},
		{"on, under, not held: nothing", PaceModeOn, false, func(*PaceInput) {}, PaceActionNone, false},
		{"shadow, over, not held: would hold", PaceModeShadow, false, over, PaceActionWouldHold, true},
		{"shadow, over, held: stays", PaceModeShadow, true, over, PaceActionNone, true},
		{"shadow, under, held: would release", PaceModeShadow, true, func(*PaceInput) {}, PaceActionWouldRelease, false},
		{"off, over: nothing", PaceModeOff, false, over, PaceActionNone, false},
		{"off, held: release", PaceModeOff, true, over, PaceActionRelease, false},
		{"an unset mode reads shadow", "", false, over, PaceActionWouldHold, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := paceInput(c.mode)
			in.Held = c.held
			c.mod(&in)
			d := EvaluatePace(in)
			if d.Action != c.wantAction || d.HeldAfter != c.wantHold {
				t.Fatalf("action=%q heldAfter=%v, want %q %v (%+v)", d.Action, d.HeldAfter, c.wantAction, c.wantHold, d)
			}
		})
	}
}

// TestEvaluatePaceHoldLifts: a hold lifts when the reading drops, when the window resets
// and when the week resets.
func TestEvaluatePaceHoldLifts(t *testing.T) {
	// Held on the window rule at 90%: a new, lower session reading lifts it.
	in := paceInput(PaceModeOn)
	in.Held, in.HeldRule = true, PaceRuleWindow
	in.WindowPercent = 90
	if d := EvaluatePace(in); !d.HeldAfter || d.Rule != PaceRuleWindow {
		t.Fatalf("still over: %+v", d)
	}
	in.WindowPercent = 40
	if d := EvaluatePace(in); d.HeldAfter || d.Action != PaceActionRelease {
		t.Fatalf("reading dropped, hold must lift: %+v", d)
	}

	// Window reset: the same 90% reading, but the clock is now past the window end, so the
	// reading belongs to an earlier window and the burn since the new window began is small.
	in = paceInput(PaceModeOn)
	in.Held, in.HeldRule = true, PaceRuleWindow
	in.WindowPercent, in.WindowReadAt = 90, paceClock.Add(-time.Hour) // 19:00, window 17:00-22:00
	in.Now = time.Date(2026, 10, 30, 22, 0, 0, 0, time.UTC)           // the next window began
	in.WindowPointsSinceStart = 0.5
	d := EvaluatePace(in)
	if d.HeldAfter || d.Action != PaceActionRelease {
		t.Fatalf("window reset, hold must lift: %+v", d)
	}
	if !strings.Contains(d.Reason, "window reset") {
		t.Fatalf("reason %q does not say the window reset", d.Reason)
	}

	// Week reset: no weekly reading in the new week yet, so the week side holds nothing.
	in = paceInput(PaceModeOn)
	in.Held, in.HeldRule = true, PaceRuleWeek
	in.WeekKnown, in.WeekReset = false, true
	in.WindowPercent = 10
	if d := EvaluatePace(in); d.HeldAfter || d.Action != PaceActionRelease || !strings.Contains(d.Reason, "week reset") {
		t.Fatalf("week reset, hold must lift: %+v", d)
	}
}

// TestEvaluatePaceUnavailableKeepsTheState: when the readings cannot be read the policy
// changes nothing, in either direction.
func TestEvaluatePaceUnavailableKeepsTheState(t *testing.T) {
	for _, held := range []bool{true, false} {
		in := paceInput(PaceModeOn)
		in.Unavailable, in.Held = true, held
		d := EvaluatePace(in)
		if d.HeldAfter != held || d.Action != PaceActionNone || d.Hold != held {
			t.Fatalf("held=%v: %+v", held, d)
		}
		if !strings.Contains(d.Reason, "unavailable") {
			t.Fatalf("reason %q", d.Reason)
		}
	}
}

func TestWindowBounds(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 30, h, m, 0, 0, time.UTC) }
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	cases := []struct {
		now          time.Time
		start, until time.Time
	}{
		{at(1, 59), day(29, 22), day(30, 2)},
		{at(2, 0), day(30, 2), day(30, 7)},
		{at(6, 59), day(30, 2), day(30, 7)},
		{at(7, 0), day(30, 7), day(30, 12)},
		{at(11, 59), day(30, 7), day(30, 12)},
		{at(12, 0), day(30, 12), day(30, 17)},
		{at(20, 0), day(30, 17), day(30, 22)},
		{at(21, 59), day(30, 17), day(30, 22)},
		{at(22, 0), day(30, 22), day(31, 2)},
		{at(23, 59), day(30, 22), day(31, 2)},
	}
	for _, c := range cases {
		s, e := WindowBounds(c.now)
		if !s.Equal(c.start) || !e.Equal(c.until) {
			t.Errorf("WindowBounds(%s) = %s to %s, want %s to %s", c.now.Format("15:04"), s.Format("02 15:04"), e.Format("02 15:04"), c.start.Format("02 15:04"), c.until.Format("02 15:04"))
		}
	}
}
