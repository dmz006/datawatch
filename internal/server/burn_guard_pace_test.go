package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// paceAdmission is the fake admission seam plus the pace-hold seam. It records every pace
// hold call so a test can prove enforce holds admission and shadow never does.
type paceAdmission struct {
	*fakeAdmission
	mu2   sync.Mutex
	calls []paceCall
	on    bool
}

type paceCall struct {
	backend string
	on      bool
	reason  string
	until   time.Time
}

func (a *paceAdmission) PaceHold(backend string, on bool, reason string, until time.Time) error {
	a.mu2.Lock()
	defer a.mu2.Unlock()
	a.calls = append(a.calls, paceCall{backend, on, reason, until})
	a.on = on
	return nil
}

func (a *paceAdmission) holdOn() bool {
	a.mu2.Lock()
	defer a.mu2.Unlock()
	return a.on
}

func (a *paceAdmission) callCount() int {
	a.mu2.Lock()
	defer a.mu2.Unlock()
	return len(a.calls)
}

// paceRig is a guard with a fake clock and fake readings. The weekly reset is Saturday 09:00
// UTC, so on Friday 20:00 the weekly pace line is about 92.3 points; the window holding 20:00
// runs 17:00 to 22:00.
type paceRig struct {
	g       *BurnGuard
	adm     *paceAdmission
	nt      *fakeNotifier
	ses     *failingSessions
	cfg     *config.BurnGuardConfig
	now     time.Time
	week    float64 // newest weekly reading, read at 19:30
	session float64 // newest session (window) reading, read at 19:30
	buckets []burnguard.Bucket
	logPath string
}

func newPaceRig(t *testing.T, mode string) *paceRig {
	t.Helper()
	cfg := rungFixtureConfig()
	cfg.PaceLineMode = mode
	// Keep the ladder quiet: it has one rung that changes nothing a pace test looks at.
	cfg.Ladder = []string{burnguard.RungLowerInflight}
	r := &paceRig{
		cfg: &cfg, now: time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC),
		week: 50, session: 10, logPath: filepath.Join(t.TempDir(), "pace.jsonl"),
		adm: &paceAdmission{fakeAdmission: newFakeAdmission(3)},
		nt:  &fakeNotifier{}, ses: &failingSessions{running: 2},
	}
	r.g = r.build(t)
	return r
}

func (r *paceRig) build(t *testing.T) *BurnGuard {
	t.Helper()
	pct := func(v float64) *float64 { return &v }
	in := BurnGuardInputs{
		Config: func() config.BurnGuardConfig { return *r.cfg },
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			at := r.now.Add(-30 * time.Minute)
			if at.After(time.Date(2026, 10, 30, 19, 30, 0, 0, time.UTC)) {
				at = time.Date(2026, 10, 30, 19, 30, 0, 0, time.UTC)
			}
			return []usagecalc.Reading{{At: at, Percent: pct(r.week), SessionPercent: pct(r.session)}}, 0,
				usagecalc.ResetSpec{Weekday: time.Saturday, Clock: "09:00", Zone: "UTC"}, nil
		},
		Tokens: func(context.Context, string, bool, time.Time) ([]burnguard.Bucket, time.Time, error) {
			return r.buckets, r.now, nil
		},
		Now:          func() time.Time { return r.now },
		PaceLogPath:  r.logPath,
		MarginPoints: func() *float64 { v := 0.0; return &v },
	}
	g, err := NewBurnGuard(in, r.adm, r.ses, r.nt, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (r *paceRig) tick() { r.g.Tick(context.Background()) }

func (r *paceRig) pace(t *testing.T) *BurnGuardPaceStatus {
	t.Helper()
	p := r.g.Status().Pace
	if p == nil || len(p.Entries) == 0 {
		t.Fatalf("status has no pace block: %+v", p)
	}
	return p
}

// TestPaceShadowRecordsAndNeverHolds: in shadow the policy computes the decision and writes
// it with its evidence, and admission is never touched.
func TestPaceShadowRecordsAndNeverHolds(t *testing.T) {
	r := newPaceRig(t, "shadow")
	r.week = 97 // line about 92.3 plus tolerance 2
	r.tick()
	r.tick()
	if r.adm.callCount() != 0 || r.adm.holdOn() {
		t.Fatalf("shadow touched admission: %+v", r.adm.calls)
	}
	p := r.pace(t)
	e := p.Entries[0]
	if e.Held || !e.WouldHold || e.Rule != burnguard.PaceRuleWeek {
		t.Fatalf("entry = %+v, want a virtual hold on the weekly line and no real hold", e)
	}
	if e.Last.Action != burnguard.PaceActionNone && e.Last.Action != burnguard.PaceActionWouldHold {
		t.Fatalf("last action = %q", e.Last.Action)
	}
	if len(p.Log) != 1 || p.Log[0].Action != burnguard.PaceActionWouldHold {
		t.Fatalf("log = %+v, want exactly one would_hold (an unchanged verdict is not logged twice)", p.Log)
	}
	ev := p.Log[0]
	if ev.WeekPercent != 97 || ev.WeekLine < 92 || ev.WeekLine > 93 || ev.WeekTolerance != 2 || ev.Mode != "shadow" {
		t.Fatalf("evidence = %+v", ev)
	}
	if p.Mode != "shadow" || p.Enforcing {
		t.Fatalf("pace status = %+v", p)
	}
	if len(r.ses.endCalls) != 0 {
		t.Fatalf("a session was ended: %v", r.ses.endCalls)
	}
}

// TestPaceEnforceHoldsAdmission: in on mode the same reading holds admission with the
// evidence on the hold, raises an alert, and a hold in force is renewed with a deadline.
func TestPaceEnforceHoldsAdmission(t *testing.T) {
	r := newPaceRig(t, "on")
	r.week = 97
	r.tick()
	if !r.adm.holdOn() || r.adm.callCount() != 1 {
		t.Fatalf("enforce did not hold: %+v", r.adm.calls)
	}
	c := r.adm.calls[0]
	if c.backend != "claude-code" || !strings.Contains(c.reason, "weekly_pace_line") || !strings.Contains(c.reason, "97.0") {
		t.Fatalf("hold = %+v, want the rule and the figure in the reason", c)
	}
	if !c.until.After(r.now) {
		t.Fatalf("hold deadline %v is not in the future", c.until)
	}
	p := r.pace(t)
	if !p.Entries[0].Held || p.Entries[0].Since == nil || !p.Enforcing {
		t.Fatalf("status = %+v", p.Entries[0])
	}
	var held bool
	for _, a := range r.nt.alerts {
		if strings.Contains(a, "Burn guard pace hold") && strings.Contains(a, "2 running session(s) were left alone") {
			held = true
		}
	}
	if !held {
		t.Fatalf("no pace-hold alert naming the running sessions: %v", r.nt.alerts)
	}
	// A tick that keeps the hold renews it (so a guard that stops ticking lets it lapse).
	r.now = r.now.Add(time.Hour)
	r.tick()
	if r.adm.callCount() != 2 || !r.adm.calls[1].on || !r.adm.calls[1].until.After(r.adm.calls[0].until) {
		t.Fatalf("hold not renewed: %+v", r.adm.calls)
	}
	if len(r.pace(t).Log) != 1 {
		t.Fatalf("a renewal is not a new decision: %+v", r.pace(t).Log)
	}
	r.ses.assertNoEnd(t)
}

// TestPaceHoldLiftsWhenTheReadingDrops: a hold lifts once a newer reading is back under the
// line, and the lift is logged with its evidence.
func TestPaceHoldLiftsWhenTheReadingDrops(t *testing.T) {
	r := newPaceRig(t, "on")
	r.week = 97
	r.tick()
	if !r.adm.holdOn() {
		t.Fatal("not held")
	}
	r.week = 60
	r.tick()
	if r.adm.holdOn() {
		t.Fatal("hold still in force after the reading dropped")
	}
	p := r.pace(t)
	if p.Entries[0].Held || len(p.Log) != 2 || p.Log[0].Action != burnguard.PaceActionRelease {
		t.Fatalf("pace = %+v", p)
	}
}

// TestPaceHoldLiftsWhenTheWindowResets: a window hold lifts at the window end with no new
// reading, because the reading belongs to the finished window.
func TestPaceHoldLiftsWhenTheWindowResets(t *testing.T) {
	r := newPaceRig(t, "on")
	r.session = 88
	r.tick()
	if !r.adm.holdOn() {
		t.Fatalf("window at 88%% did not hold: %+v", r.pace(t).Entries[0].Last)
	}
	if got := r.pace(t).Entries[0].Rule; got != burnguard.PaceRuleWindow {
		t.Fatalf("rule = %q", got)
	}
	r.now = time.Date(2026, 10, 30, 22, 5, 0, 0, time.UTC) // the 22:00 window began
	r.tick()
	if r.adm.holdOn() {
		t.Fatalf("hold survived the window reset: %+v", r.pace(t).Entries[0].Last)
	}
	last := r.pace(t).Log[0]
	if last.Action != burnguard.PaceActionRelease || !last.WindowReset || !strings.Contains(last.Reason, "window reset") {
		t.Fatalf("lift not logged as a window reset: %+v", last)
	}
}

// TestPaceWindowUsesTheBurnSinceTheReading: the window estimate adds the points the tokens
// say were used after the session reading, at window_percent_per_point.
func TestPaceWindowUsesTheBurnSinceTheReading(t *testing.T) {
	r := newPaceRig(t, "on")
	r.session = 70
	// 19:00 bucket, 10M cache-write tokens: 3.73 points, half of them after the 19:30 reading.
	r.buckets = []burnguard.Bucket{{Hour: time.Date(2026, 10, 30, 19, 0, 0, 0, time.UTC), Other: burnguard.Tokens{CacheWrite: 10_000_000}}}
	r.tick()
	e := r.pace(t).Entries[0].Last
	if e.WindowPointsSince < 1.5 || e.WindowPointsSince > 2.2 {
		t.Fatalf("points since the reading = %v, want about 1.87", e.WindowPointsSince)
	}
	// 70 + about 1.87 * 4 is about 77.5, under the 85 pause line.
	if e.Hold {
		t.Fatalf("held at an estimate of %.1f%%: %+v", e.WindowEstimated, e)
	}
	r.session = 80 // 80 + 7.5 is over 85
	r.tick()
	if !r.adm.holdOn() {
		t.Fatalf("not held at an estimate of %.1f%%", r.pace(t).Entries[0].Last.WindowEstimated)
	}
}

// TestPaceModeChangeReleasesARealHold: leaving on mode never leaves admission restricted.
func TestPaceModeChangeReleasesARealHold(t *testing.T) {
	for _, to := range []string{"shadow", "off"} {
		r := newPaceRig(t, "on")
		r.week = 97
		r.tick()
		if !r.adm.holdOn() {
			t.Fatal("not held")
		}
		r.cfg.PaceLineMode = to
		r.tick()
		if r.adm.holdOn() {
			t.Fatalf("mode %s left the hold in force", to)
		}
		if p := r.pace(t); p.Entries[0].Held || p.Enforcing {
			t.Fatalf("mode %s: %+v", to, p)
		}
	}
}

func TestPaceGuardOffReleases(t *testing.T) {
	r := newPaceRig(t, "on")
	r.week = 97
	r.tick()
	r.cfg.Enabled = false
	r.tick()
	if r.adm.holdOn() {
		t.Fatal("a guard that is off left the pace hold in force")
	}
}

// TestPaceOffDecidesNothing: off neither holds nor logs a would-be hold.
func TestPaceOffDecidesNothing(t *testing.T) {
	r := newPaceRig(t, "off")
	r.week = 97
	r.tick()
	if r.adm.callCount() != 0 {
		t.Fatalf("off touched admission: %+v", r.adm.calls)
	}
	if p := r.g.Status().Pace; p == nil || len(p.Log) != 0 {
		t.Fatalf("off logged: %+v", p)
	}
}

// TestPaceUnsetModeReadsShadow: the default in code is shadow.
func TestPaceUnsetModeReadsShadow(t *testing.T) {
	if config.BurnGuardModeDefault != "shadow" || config.DefaultBurnGuard().PaceLineMode != "shadow" {
		t.Fatal("the pace policy must default to shadow in code")
	}
	r := newPaceRig(t, "")
	r.week = 97
	r.tick()
	if r.adm.callCount() != 0 {
		t.Fatal("an unset mode held admission")
	}
}

// TestPaceLogSurvivesARestart: every decision is appended to the log file with its evidence
// and a new guard shows the recent ones.
func TestPaceLogSurvivesARestart(t *testing.T) {
	r := newPaceRig(t, "on")
	r.week = 97
	r.tick()
	b, err := os.ReadFile(r.logPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec burnguard.PaceDecision
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(b))), &rec); err != nil {
		t.Fatalf("log line is not a decision: %v: %s", err, b)
	}
	if rec.Rule != burnguard.PaceRuleWeek || rec.WeekPercent != 97 || rec.Action != burnguard.PaceActionHold {
		t.Fatalf("logged = %+v", rec)
	}
	g2 := r.build(t)
	if p := g2.Status().Pace; p == nil || len(p.Log) != 1 || p.Log[0].Rule != burnguard.PaceRuleWeek {
		t.Fatalf("restarted guard shows %+v", g2.Status().Pace)
	}
}

// admissionWithoutPaceSeam has no PaceHold method: enforce cannot take effect, and the
// guard says so instead of pretending.
type admissionWithoutPaceSeam struct{ *fakeAdmission }

func TestPaceEnforceWithoutASeamReportsAnError(t *testing.T) {
	r := newPaceRig(t, "on")
	r.week = 97
	cfg := *r.cfg
	in := BurnGuardInputs{
		Config: func() config.BurnGuardConfig { return cfg },
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			v := 97.0
			return []usagecalc.Reading{{At: r.now.Add(-30 * time.Minute), Percent: &v}}, 0, usagecalc.ResetSpec{Weekday: time.Saturday, Clock: "09:00", Zone: "UTC"}, nil
		},
		Tokens: func(context.Context, string, bool, time.Time) ([]burnguard.Bucket, time.Time, error) {
			return nil, r.now, nil
		},
		Now: func() time.Time { return r.now },
	}
	g, err := NewBurnGuard(in, admissionWithoutPaceSeam{newFakeAdmission(3)}, r.ses, r.nt, nil)
	if err != nil {
		t.Fatal(err)
	}
	g.Tick(context.Background())
	st := g.Status()
	if st.Pace == nil || st.Pace.Entries[0].Held || !strings.Contains(st.LastError, "pace") {
		t.Fatalf("status = %+v, last error %q", st.Pace, st.LastError)
	}
}
