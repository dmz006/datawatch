package burnguard

import (
	"fmt"
	"strings"
	"time"
)

// The pace policy is the burn guard's hard line, beside the ladder. The ladder is graded and
// works from the projection to the weekly reset: it lowers lanes, routes the entry's new work,
// and only at its last rung holds. The pace policy works from the readings as they stand: it
// holds NEW work when the current window is at the operator's pause line (default 85% of the
// window) or the week is ahead of the weekly pace line by more than the tolerance, and lifts the
// hold when the reading drops or the window or the week resets. It changes admission only; a
// running session always finishes.
//
// EvaluatePace is a pure function: it reads no clock, no file and no setting, so a test drives
// every rung with a fake clock and fake readings.

// Pace policy modes (usage.burn_guard.mode). off decides nothing; shadow decides and records
// what it would do without touching admission; on holds and lifts.
const (
	PaceModeOff    = "off"
	PaceModeShadow = "shadow"
	PaceModeOn     = "on"
)

// Pace rules, the name of the line that holds.
const (
	PaceRuleWindow = "window_pause_line"
	PaceRuleWeek   = "weekly_pace_line"
)

// Pace actions, what the evaluation did to the hold. The would_ forms are the shadow path.
const (
	PaceActionNone         = "none"
	PaceActionHold         = "hold"
	PaceActionRelease      = "release"
	PaceActionWouldHold    = "would_hold"
	PaceActionWouldRelease = "would_release"
)

// PaceSettings are the pace policy's settings (usage.burn_guard.mode and its five siblings).
type PaceSettings struct {
	// Mode is off, shadow or on; an unset or unknown mode reads shadow.
	Mode string
	// WeekTolerance is how many points the week may run over the pace line and still be ok.
	WeekTolerance float64
	// PointsToWeekFactor counts one point of burn since the weekly reading as this many points
	// of the weekly allowance.
	PointsToWeekFactor float64
	// WindowPercentPerPoint is how many percent of the current window one point is.
	WindowPercentPerPoint float64
	// WindowStopPercent is the pause line: the percent of a window at which new work is held.
	WindowStopPercent float64
	// ReadingFreshnessMinutes is how old a reading may be before the decision says it is stale.
	ReadingFreshnessMinutes int
}

// NormalizePaceMode maps a mode to one of the three: an unset or unknown mode reads shadow.
func NormalizePaceMode(mode string) string {
	switch m := strings.ToLower(strings.TrimSpace(mode)); m {
	case PaceModeOff, PaceModeOn, PaceModeShadow:
		return m
	}
	return PaceModeShadow
}

// PaceInput is what one evaluation reads.
type PaceInput struct {
	Entry    string
	Settings PaceSettings
	Now      time.Time

	// Unavailable is true when the readings could not be read at all: the evaluation then
	// changes nothing.
	Unavailable bool

	// Week side. WeekKnown is false when there is no usable weekly reading (none yet, or the
	// newest is from before the last reset); WeekReset says it is the reset that made it so.
	WeekKnown  bool
	WeekReset  bool
	WeekReadAt time.Time
	// WeekPercent is the newest weekly reading; WeekPointsSince the points burned since it;
	// LineNow the weekly pace line at Now.
	WeekPercent     float64
	WeekPointsSince float64
	LineNow         float64

	// Window side. WindowKnown is false when no reading carries a session (window) percent.
	WindowKnown   bool
	WindowReadAt  time.Time
	WindowPercent float64 // the newest session reading
	// WindowPointsSince is the points burned since that reading; WindowPointsSinceStart the
	// points burned since the current window began, used when the reading is from an earlier
	// window.
	WindowPointsSince      float64
	WindowPointsSinceStart float64

	// Held is whether the policy holds new work now (in shadow, the virtual hold it would
	// hold); HeldRule is the rule that holds it.
	Held     bool
	HeldRule string
}

// PaceDecision is one evaluation with the evidence it was made on. Every field a surface
// shows comes from here.
type PaceDecision struct {
	At    time.Time `json:"at"`
	Entry string    `json:"entry"`
	Mode  string    `json:"mode"`
	// Hold is the verdict: the policy wants new work held (whatever the mode). Rule is the
	// line that says so.
	Hold bool   `json:"hold"`
	Rule string `json:"rule,omitempty"`
	// Action is what was done to the hold; HeldAfter whether the policy holds afterwards.
	Action    string `json:"action"`
	HeldAfter bool   `json:"held_after"`
	Reason    string `json:"reason"`

	WeekReadAt      time.Time `json:"week_read_at,omitempty"`
	WeekPercent     float64   `json:"week_percent"`
	WeekPointsSince float64   `json:"week_points_since"`
	WeekEstimated   float64   `json:"week_estimated"`
	WeekLine        float64   `json:"week_line"`
	WeekTolerance   float64   `json:"week_tolerance"`
	WeekReset       bool      `json:"week_reset,omitempty"`

	WindowReadAt      time.Time `json:"window_read_at,omitempty"`
	WindowPercent     float64   `json:"window_percent"`
	WindowPointsSince float64   `json:"window_points_since"`
	WindowEstimated   float64   `json:"window_estimated"`
	WindowStopPercent float64   `json:"window_stop_percent"`
	WindowStart       time.Time `json:"window_start"`
	WindowEnd         time.Time `json:"window_end"`
	WindowReset       bool      `json:"window_reset,omitempty"`

	ReadingAgeMinutes int  `json:"reading_age_minutes"`
	Stale             bool `json:"stale,omitempty"`
}

// windowAnchorHours are the hours at which a window ends and the next begins. The windows
// are fixed, not a setting: they end at 02:00, 07:00 and 12:00, then every five hours, so
// the last window of the day (22:00 to 02:00) is four hours long.
var windowAnchorHours = []int{2, 7, 12, 17, 22}

// WindowBounds returns the start and the end of the window that holds now, in now's zone.
func WindowBounds(now time.Time) (start, end time.Time) {
	y, m, d := now.Date()
	loc := now.Location()
	at := func(dayOffset, hour int) time.Time { return time.Date(y, m, d+dayOffset, hour, 0, 0, 0, loc) }
	start = at(-1, windowAnchorHours[len(windowAnchorHours)-1])
	for _, h := range windowAnchorHours {
		if t := at(0, h); !t.After(now) {
			start = t
		}
	}
	end = at(1, windowAnchorHours[0])
	for _, h := range windowAnchorHours {
		if t := at(0, h); t.After(now) {
			end = t
			break
		}
	}
	return start, end
}

// EvaluatePace decides whether new work should be held and what to do about the hold that is
// in force. In mode off nothing is held and a hold in force is released. In shadow the same
// decision is made on the caller's virtual hold and the action carries the would_ names, so
// the log compares with what on would have done. In on the action is the real one.
func EvaluatePace(in PaceInput) PaceDecision {
	s := in.Settings
	mode := NormalizePaceMode(s.Mode)
	factor := s.PointsToWeekFactor
	if factor <= 0 {
		factor = 1
	}
	d := PaceDecision{
		At: in.Now, Entry: in.Entry, Mode: mode, Action: PaceActionNone,
		WeekReadAt: in.WeekReadAt, WeekPercent: in.WeekPercent, WeekPointsSince: in.WeekPointsSince,
		WeekLine: in.LineNow, WeekTolerance: s.WeekTolerance, WeekReset: in.WeekReset,
		WindowReadAt: in.WindowReadAt, WindowPercent: in.WindowPercent, WindowPointsSince: in.WindowPointsSince,
		WindowStopPercent: s.WindowStopPercent,
	}
	d.WindowStart, d.WindowEnd = WindowBounds(in.Now)

	if in.Unavailable {
		d.Hold, d.Rule, d.HeldAfter = in.Held, in.HeldRule, in.Held
		d.Reason = "the readings are unavailable; the hold is left as it was"
		return d
	}

	// Reading age and staleness: informational. The estimate carries the burn since the
	// reading, so a stale reading is still decided on; the reason asks for a fresh one.
	age := time.Duration(-1)
	for _, r := range []struct {
		known bool
		at    time.Time
	}{{in.WeekKnown, in.WeekReadAt}, {in.WindowKnown, in.WindowReadAt}} {
		if !r.known || r.at.IsZero() {
			continue
		}
		a := in.Now.Sub(r.at)
		if age < 0 || a < age {
			age = a
		}
		if s.ReadingFreshnessMinutes > 0 && a > time.Duration(s.ReadingFreshnessMinutes)*time.Minute {
			d.Stale = true
		}
	}
	if age >= 0 {
		d.ReadingAgeMinutes = int(age / time.Minute)
	}

	var overWindow, overWeek bool
	var why []string
	if in.WindowKnown {
		if in.WindowReadAt.Before(d.WindowStart) {
			// The reading belongs to an earlier window: only the burn since this one began counts.
			d.WindowReset = true
			d.WindowEstimated = in.WindowPointsSinceStart * s.WindowPercentPerPoint
			why = append(why, fmt.Sprintf("window reset: the window reading is from before %s, so only the burn since then counts (%.1f%% of the window)", d.WindowStart.Format("15:04"), d.WindowEstimated))
		} else {
			d.WindowEstimated = in.WindowPercent + in.WindowPointsSince*s.WindowPercentPerPoint
		}
		overWindow = s.WindowStopPercent > 0 && d.WindowEstimated >= s.WindowStopPercent
	}
	if in.WeekKnown {
		d.WeekEstimated = in.WeekPercent + in.WeekPointsSince*factor
		overWeek = d.WeekEstimated > in.LineNow+s.WeekTolerance
	} else if in.WeekReset {
		why = append(why, "week reset: no weekly reading in the new week yet")
	}

	switch {
	case overWindow:
		d.Hold, d.Rule = true, PaceRuleWindow
		d.Reason = fmt.Sprintf("window estimate %.1f%% is at or over the %.0f%% pause line", d.WindowEstimated, s.WindowStopPercent)
	case overWeek:
		d.Hold, d.Rule = true, PaceRuleWeek
		d.Reason = fmt.Sprintf("week estimate %.1f%% is over the pace line %.1f%% plus %.1f points of tolerance", d.WeekEstimated, in.LineNow, s.WeekTolerance)
	default:
		d.Reason = "under the window pause line and the weekly pace line"
	}
	if len(why) > 0 {
		d.Reason += "; " + strings.Join(why, "; ")
	}
	if d.Stale {
		d.Reason += fmt.Sprintf("; the newest reading is %d minutes old, send a fresh reading", d.ReadingAgeMinutes)
	}

	// Mode and the hold in force give the action. Off holds nothing.
	want := d.Hold
	if mode == PaceModeOff {
		want = false
	}
	switch {
	case want && !in.Held:
		d.Action = pick(mode == PaceModeOn, PaceActionHold, PaceActionWouldHold)
	case !want && in.Held:
		d.Action = pick(mode == PaceModeOn || mode == PaceModeOff, PaceActionRelease, PaceActionWouldRelease)
		if mode == PaceModeOff {
			d.Reason = "the policy is off; the hold is released"
		}
	}
	d.HeldAfter = want
	return d
}

func pick(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

// PointsBetween is the points the tokens in [from, to) are priced at with c. through is where
// the token data ends (see Usage.Through); zero means to.
func PointsBetween(buckets []Bucket, from, to, through time.Time, c Coefficients) float64 {
	if through.IsZero() || through.After(to) {
		through = to
	}
	return price(tokensBetween(buckets, from, to, through), c)
}
