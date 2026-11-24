// burn_guard_pace.go — the pace policy half of the burn guard runner. Beside the ladder (which
// works from the projection to the weekly reset) the runner decides, from the readings as they
// stand, whether new work should be held: the current window is at the operator's pause line,
// or the week is ahead of the weekly pace line by more than the tolerance. usage.burn_guard.mode
// picks what is done with that decision: off decides nothing, shadow computes it and records it
// with its evidence without touching admission, on holds new work and lifts the hold when the
// reading drops or the window or the week resets. Running sessions are never touched, and every
// decision that changes the verdict or the hold is kept in a bounded log, in memory and in a
// JSON-lines file, for the status, the web app, the dashboard and the other surfaces.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// BurnGuardPaceHold is the optional admission seam the pace policy drives in mode on: it holds
// (on) or releases (off) new work for the backend. until is the hold's deadline; the runner
// renews it on every tick that keeps the hold, so a runner that stops ticking lets it lapse.
// Like every seam of the guard it may not touch a running session. Without it the policy in
// mode on cannot take effect, and the runner reports that as an error.
type BurnGuardPaceHold interface {
	PaceHold(backend string, on bool, reason string, until time.Time) error
}

// paceLogKeep is how many decisions the status carries.
const paceLogKeep = 100

// BurnGuardPaceStatus is the pace policy block of the status.
type BurnGuardPaceStatus struct {
	// Mode is off, shadow or on (an unset mode reads shadow). Enforcing is true only in on.
	Mode      string `json:"mode"`
	Enforcing bool   `json:"enforcing"`
	// Held is true when any entry's new work is held now (a real hold, mode on only).
	Held    bool                 `json:"held"`
	Entries []BurnGuardPaceEntry `json:"entries"`
	// Log is the recent decisions that changed the verdict or the hold, newest first, each
	// with the evidence it was made on.
	Log []burnguard.PaceDecision `json:"log"`
}

// BurnGuardPaceEntry is one entry's pace state: Held is a real hold, WouldHold the hold the
// shadow path would have in force.
type BurnGuardPaceEntry struct {
	LLM       string                 `json:"llm"`
	Held      bool                   `json:"held"`
	WouldHold bool                   `json:"would_hold"`
	Rule      string                 `json:"rule,omitempty"`
	Since     *time.Time             `json:"since,omitempty"`
	Last      burnguard.PaceDecision `json:"last"`
}

// paceState is the runner's memory of one entry's pace policy.
type paceState struct {
	real, virtual bool
	rule          string
	since         time.Time
	last          burnguard.PaceDecision
	lastKey       string
}

func (g *BurnGuard) paceStatusLocked() *BurnGuardPaceStatus {
	mode := burnguard.NormalizePaceMode(g.in.Config().PaceLineMode)
	out := &BurnGuardPaceStatus{Mode: mode, Enforcing: mode == burnguard.PaceModeOn, Entries: []BurnGuardPaceEntry{},
		Log: append([]burnguard.PaceDecision{}, g.paceLog...)}
	for _, name := range g.paceOrder {
		st := g.pace[name]
		e := BurnGuardPaceEntry{LLM: name, Held: st.real, WouldHold: st.virtual && !st.real, Last: st.last}
		if st.real || st.virtual {
			e.Rule = st.rule
		}
		if st.real {
			t := st.since
			e.Since = &t
		}
		out.Held = out.Held || st.real
		out.Entries = append(out.Entries, e)
	}
	return out
}

func (g *BurnGuard) paceStateFor(name string) *paceState {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pace == nil {
		g.pace = map[string]*paceState{}
	}
	st, ok := g.pace[name]
	if !ok {
		st = &paceState{}
		g.pace[name] = st
		g.paceOrder = append(g.paceOrder, name)
	}
	return st
}

// paceSettings reads the pace policy settings off the guard config.
func paceSettings(cfg config.BurnGuardConfig, mode string) burnguard.PaceSettings {
	return burnguard.PaceSettings{
		Mode: mode, WeekTolerance: float64(cfg.PaceLineWeekTolerance), PointsToWeekFactor: cfg.PaceLinePointsToWeekFactor,
		WindowPercentPerPoint: cfg.PaceLineWindowPercentPerPoint, WindowStopPercent: cfg.PaceLineWindowStopPercent,
		ReadingFreshnessMinutes: cfg.PaceLineReadingFreshnessMinutes,
	}
}

// paceHoldFor is the seam, or an error when the admission seam has none.
func (g *BurnGuard) paceSeam() (BurnGuardPaceHold, error) {
	if ph, ok := g.adm.(BurnGuardPaceHold); ok {
		return ph, nil
	}
	return nil, fmt.Errorf("pace hold: no pace-hold seam wired on the admission seam")
}

// tickPace runs the pace policy for one entry after its ladder tick. enabled is whether the
// guard applies to the entry at all; when it does not (the guard is off, the entry has no usage
// source or is not guarded) a hold left in force is released, like the ladder's rungs.
func (g *BurnGuard) tickPace(cfg config.BurnGuardConfig, e config.BurnGuardEntryConfig, first, enabled bool, es *guardedEntry,
	proj burnguard.Projection, now time.Time, readings func() ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error)) {
	mode := burnguard.NormalizePaceMode(cfg.PaceLineMode)
	st := g.paceStateFor(e.LLM)
	// A metered entry has no weekly percentage to hold on: it is not paced.
	if !enabled || e.IsMetered() {
		mode = burnguard.PaceModeOff
	}
	set := paceSettings(cfg, mode)

	// Leaving mode on (shadow, off, a guard that is off) never leaves a hold in force.
	if mode != burnguard.PaceModeOn && st.real {
		g.paceLeaveOn(cfg, e, st, set, mode, now)
	}
	if mode == burnguard.PaceModeOff {
		g.paceSet(st, func(st *paceState) { st.virtual = false })
		return
	}

	all, _, reset, rerr := readings()
	in := burnguard.PaceInput{Entry: e.LLM, Settings: set, Now: now, Held: st.virtual, HeldRule: st.rule}
	if mode == burnguard.PaceModeOn {
		in.Held = st.real
	}
	if rerr != nil || proj.Needs == "readings unavailable" {
		in.Unavailable = true
	} else {
		in.Now = now.In(paceZone(reset, now))
		g.paceFill(&in, all, e, first, es, proj)
	}
	d := burnguard.EvaluatePace(in)
	g.paceApply(cfg, e, st, mode, d, now)
}

// paceFill puts the readings, the burn since them and the pace line on the input.
func (g *BurnGuard) paceFill(in *burnguard.PaceInput, all []usagecalc.Reading, e config.BurnGuardEntryConfig, first bool, es *guardedEntry, proj burnguard.Projection) {
	rs := usagecalc.ReadingsForEntry(all, e.LLM, first)
	hasPercent := false
	var window *usagecalc.Reading
	for i := range rs {
		if rs[i].Percent != nil {
			hasPercent = true
		}
		if rs[i].SessionPercent != nil && (window == nil || rs[i].At.After(window.At)) {
			window = &rs[i]
		}
	}
	switch {
	case proj.Needs == "":
		in.WeekKnown = true
		in.WeekReadAt, in.WeekPercent = proj.ReadAt, proj.PercentAtReading
		in.WeekPointsSince, in.LineNow = proj.PointsSinceReading, proj.LineNow
	case proj.Needs == usagecalc.NeedsReading && hasPercent:
		in.WeekReset = true // readings exist, none from this week: the week reset
	}
	if window == nil {
		return
	}
	ratio := proj.CalibrationRatio
	if ratio <= 0 {
		ratio = 1
	}
	start, _ := burnguard.WindowBounds(in.Now)
	in.WindowKnown, in.WindowReadAt, in.WindowPercent = true, window.At, *window.SessionPercent
	in.WindowPointsSince = burnguard.PointsBetween(es.buckets, window.At, in.Now, es.through, es.coef) * ratio
	in.WindowPointsSinceStart = burnguard.PointsBetween(es.buckets, start, in.Now, es.through, es.coef) * ratio
}

// paceZone is the zone the windows are read in: the weekly reset's zone, else the clock's.
func paceZone(reset usagecalc.ResetSpec, now time.Time) *time.Location {
	if reset.Zone != "" {
		if loc, err := time.LoadLocation(reset.Zone); err == nil {
			return loc
		}
	}
	return now.Location()
}

// paceLeaveOn releases a real hold because the mode is no longer on, and records it.
func (g *BurnGuard) paceLeaveOn(cfg config.BurnGuardConfig, e config.BurnGuardEntryConfig, st *paceState, set burnguard.PaceSettings, mode string, now time.Time) {
	off := set
	off.Mode = burnguard.PaceModeOff
	d := burnguard.EvaluatePace(burnguard.PaceInput{Entry: e.LLM, Settings: off, Now: now, Held: true, HeldRule: st.rule})
	d.Mode = mode
	d.Reason = "the pace policy is not enforcing (mode " + mode + "); the hold is released"
	was := st.rule
	g.paceApply(cfg, e, st, burnguard.PaceModeOn, d, now)
	// A shadow policy carries on from the hold it had.
	g.paceSet(st, func(st *paceState) { st.virtual, st.rule = mode == burnguard.PaceModeShadow, was })
}

// paceApply carries out one decision: it moves the hold on the seam in mode on, keeps the
// state, records the decision when it changed something, and alerts on a real change.
func (g *BurnGuard) paceApply(cfg config.BurnGuardConfig, e config.BurnGuardEntryConfig, st *paceState, mode string, d burnguard.PaceDecision, now time.Time) {
	reason := fmt.Sprintf("%s: %s", d.Rule, d.Reason)
	until := now.Add(2 * g.interval(cfg))
	switch {
	case mode == burnguard.PaceModeOn && d.Action == burnguard.PaceActionRelease:
		ph, err := g.paceSeam()
		if err == nil {
			err = ph.PaceHold(e.LLM, false, "", time.Time{})
		}
		if err != nil {
			g.fail("%s: %v", e.LLM, err)
			d.Reason += " (release not applied: " + err.Error() + ")"
			d.HeldAfter = true
		} else {
			g.paceSet(st, func(st *paceState) { st.real = false })
		}
	case mode == burnguard.PaceModeOn && (d.Action == burnguard.PaceActionHold || (d.Action == burnguard.PaceActionNone && d.HeldAfter)):
		ph, err := g.paceSeam()
		if err == nil {
			err = ph.PaceHold(e.LLM, true, reason, until)
		}
		if err != nil {
			g.fail("%s: %v", e.LLM, err)
			d.Reason += " (hold not applied: " + err.Error() + ")"
			d.HeldAfter = false
		} else if !st.real {
			g.paceSet(st, func(st *paceState) { st.real, st.since = true, now })
		}
	}
	key := fmt.Sprintf("%v|%s", d.Hold, d.Rule)
	var changed bool
	g.paceSet(st, func(st *paceState) {
		if mode == burnguard.PaceModeOn {
			st.virtual = st.real
		} else {
			st.virtual = d.HeldAfter
		}
		if st.real || st.virtual {
			if d.Rule != "" {
				st.rule = d.Rule
			}
		} else {
			st.rule = ""
		}
		st.last = d
		changed = d.Action != burnguard.PaceActionNone || key != st.lastKey
		st.lastKey = key
	})
	if !changed {
		return
	}
	g.paceRecord(d)
	if mode == burnguard.PaceModeOn && (d.Action == burnguard.PaceActionHold || d.Action == burnguard.PaceActionRelease) && g.notify != nil {
		running := ""
		if g.sessions != nil {
			running = fmt.Sprintf(" %d running session(s) were left alone.", g.sessions.RunningCount())
		}
		title := "Burn guard pace " + d.Action + ": " + d.Rule + " (" + e.LLM + ")"
		if d.Action == burnguard.PaceActionRelease {
			title = "Burn guard pace release (" + e.LLM + ")"
		}
		g.notify.Alert(alerts.LevelWarn, title, d.Reason+running)
	}
}

// paceSet changes a paceState under the runner's lock, so a status read never races a tick.
func (g *BurnGuard) paceSet(st *paceState, f func(*paceState)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f(st)
}

func (g *BurnGuard) interval(cfg config.BurnGuardConfig) time.Duration {
	mins := cfg.IntervalMinutes
	if mins < 1 {
		mins = config.BurnGuardIntervalMinutesDefault
	}
	return time.Duration(mins) * time.Minute
}

// paceRecord keeps a decision in the bounded log and appends it to the log file.
func (g *BurnGuard) paceRecord(d burnguard.PaceDecision) {
	g.mu.Lock()
	g.paceLog = append([]burnguard.PaceDecision{d}, g.paceLog...)
	if len(g.paceLog) > paceLogKeep {
		g.paceLog = g.paceLog[:paceLogKeep]
	}
	path := g.in.PaceLogPath
	g.mu.Unlock()
	if path == "" {
		return
	}
	if err := appendPaceLog(path, d); err != nil {
		g.fail("pace log not written: %v", err)
	}
}

func appendPaceLog(path string, d burnguard.PaceDecision) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// loadPaceLog reads the newest paceLogKeep decisions of the log file, newest first. A missing
// file is an empty log and an unreadable line is skipped.
func loadPaceLog(path string) []burnguard.PaceDecision {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []burnguard.PaceDecision
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var d burnguard.PaceDecision
		if json.Unmarshal(sc.Bytes(), &d) == nil && d.Action != "" {
			out = append(out, d)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) > paceLogKeep {
		out = out[:paceLogKeep]
	}
	return out
}
