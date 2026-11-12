// burn_guard.go — the burn guard runner. Every usage.burn_guard.interval_minutes it reads
// the Claude transcripts, prices the tokens in weekly points, projects the burn to the
// weekly reset against the pace line of the usage calculator, asks the ladder
// (internal/burnguard) for a Decision and applies it:
//
//	lower_inflight   lower the claude-code inflight cap by one (the original is remembered
//	                 and restored on release)
//	route_pi_prompt  route new claude-code work to pi-prompt
//	hold_claude      hold new claude-code work
//
// Every action acts on admission only. The runner has no way to end a session: its session
// view (BurnGuardSessions) is read-only, and the ladder has no action kind that kills,
// stops or restarts one. A running session always finishes.
//
// Each change raises an alert and a queue question carrying the numbers and what was done.
// A new usage reading refits the cost model and runs one tick at once (OnReading).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// BurnGuardAdmission is the daemon's admission seam. It is the only thing the runner
// changes. None of its methods may touch a running session.
type BurnGuardAdmission interface {
	// InflightCap is the inflight cap now in force for the backend.
	InflightCap(backend string) int
	// SetInflight sets the cap; the daemon keeps it until it is set again.
	SetInflight(backend string, n int) error
	// RouteBackend routes (on) or stops routing (off) new work meant for from to to.
	RouteBackend(from, to string, on bool) error
	// HoldNewWork holds (on) or releases (off) admission of new work for the backend.
	HoldNewWork(backend string, on bool) error
}

// BurnGuardSessions is a read-only view of the session manager. It deliberately has no
// method that ends a session; the test for the runner hands it a manager that fails when
// any end call is made.
type BurnGuardSessions interface {
	// RunningCount is the number of sessions running now.
	RunningCount() int
}

// BurnGuardInputs is where one tick reads its data from. A test replaces the fields.
type BurnGuardInputs struct {
	// Config returns the live usage.burn_guard settings.
	Config func() config.BurnGuardConfig
	// Readings returns the percentage readings, the bought extra credit and the weekly reset.
	Readings func(ctx context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error)
	// MarginPoints returns the projection margin (the calculator's safety points); nil
	// means the default.
	MarginPoints func() *float64
	// TranscriptDir is the directory of Claude transcripts (~/.claude/projects layout).
	TranscriptDir string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Applies reports whether the guard acts on the backend: true for an allowance
	// entry, and for an api or local entry only with a dollar budget set. nil means
	// it always applies (the behaviour before billing modes).
	Applies func(backend string) bool
}

// BurnGuardNotifier raises the alert and the queue question of one step.
type BurnGuardNotifier interface {
	Alert(level alerts.Level, title, body string)
	Question(text string) error
}

// BurnGuardStatus is what the surfaces show.
type BurnGuardStatus struct {
	Enabled bool `json:"enabled"`
	// State is "off", "idle" (no rung in force), "guarding" (one or more rungs in force) or
	// "waiting" (enabled, no usable projection yet).
	State string `json:"state"`
	Level int    `json:"level"`
	// Rung is the newest rung in force ("" when none); Line and Margin are the pace
	// line and margin of the last projection (nil before there is one).
	Rung           string                 `json:"rung,omitempty"`
	Line           *float64               `json:"line,omitempty"`
	Margin         *float64               `json:"margin,omitempty"`
	Applied        []string               `json:"applied,omitempty"`
	LastTickAt     *time.Time             `json:"last_tick_at,omitempty"`
	LastChangeAt   *time.Time             `json:"last_change_at,omitempty"`
	LastProjection *burnguard.Projection  `json:"last_projection,omitempty"`
	LastDecision   *burnguard.Decision    `json:"last_decision,omitempty"`
	Coefficients   burnguard.Coefficients `json:"coefficients"`
	LastFit        *burnguard.FitResult   `json:"last_fit,omitempty"`
	LastError      string                 `json:"last_error,omitempty"`
	Counters       BurnGuardCounters      `json:"counters"`
}

// BurnGuardCounters are the monotonic counts since the daemon started.
type BurnGuardCounters struct {
	Ticks       int64 `json:"ticks"`
	Climbs      int64 `json:"climbs"`
	StepBacks   int64 `json:"step_backs"`
	Holds       int64 `json:"holds"`
	Refits      int64 `json:"refits"`
	Errors      int64 `json:"errors"`
	ActionsDone int64 `json:"actions_applied"`
}

// BurnGuard is the runner. Tick is safe to call from several goroutines; ticks are
// serialised.
type BurnGuard struct {
	in       BurnGuardInputs
	adm      BurnGuardAdmission
	sessions BurnGuardSessions
	notify   BurnGuardNotifier
	ladder   *burnguard.Ladder
	fitter   *burnguard.Fitter

	tickMu sync.Mutex // serialises ticks

	mu            sync.Mutex // guards the fields below
	status        BurnGuardStatus
	refitWake     chan struct{}
	questionStore autonomous.BurnGuardQuestionStore
	rungSink      func(oldRung, newRung string)
}

// NewBurnGuard builds the runner. store may be nil (no persistence across restarts).
func NewBurnGuard(in BurnGuardInputs, adm BurnGuardAdmission, sessions BurnGuardSessions, notify BurnGuardNotifier, store burnguard.StateStore) (*BurnGuard, error) {
	if in.Config == nil || in.Readings == nil {
		return nil, errors.New("burn guard: config and readings sources are required")
	}
	if adm == nil {
		return nil, errors.New("burn guard: no admission seam wired")
	}
	if in.Now == nil {
		in.Now = time.Now
	}
	ladder, err := burnguard.NewLadder(store)
	if err != nil {
		return nil, err
	}
	cfg := in.Config()
	g := &BurnGuard{
		in: in, adm: adm, sessions: sessions, notify: notify, ladder: ladder,
		fitter: burnguard.NewFitter(burnguard.Coefficients{
			CacheWritePerM: cfg.CacheWritePointsPerMillion, CacheReadPerM: cfg.CacheReadPointsPerMillion,
		}),
		refitWake: make(chan struct{}, 1),
	}
	g.status.Coefficients = g.fitter.Coefficients()
	return g, nil
}

// AttachQuestionStore wires the daemon's question store so the guard's
// ask_on_change handling runs: it withdraws the guard's open question before
// raising a new one (at most one open), and when ask_on_change is off it
// withdraws the open question instead of raising. Without one, a question goes
// through the notifier as before.
func (g *BurnGuard) AttachQuestionStore(st autonomous.BurnGuardQuestionStore) {
	g.mu.Lock()
	g.questionStore = st
	g.mu.Unlock()
}

// AttachRungSink wires the receiver of rung changes: it is called with the
// rung in force before and after each changed tick (empty means none).
func (g *BurnGuard) AttachRungSink(fn func(oldRung, newRung string)) {
	g.mu.Lock()
	g.rungSink = fn
	g.mu.Unlock()
}

// currentRung is the rung in force for an applied list: the last one applied.
func currentRung(applied []string) string {
	if len(applied) == 0 {
		return ""
	}
	return applied[len(applied)-1]
}

// LadderState is the persisted ladder state as it was loaded at start, which
// the daemon consults when it prunes guard questions at start.
func (g *BurnGuard) LadderState() burnguard.LadderState { return g.ladder.State() }

// Run ticks every interval_minutes until ctx is cancelled, and once more at once whenever
// OnReading signals a new reading. The interval is read each round, so a config change
// applies without a restart.
func (g *BurnGuard) Run(ctx context.Context) {
	for {
		g.Tick(ctx)
		mins := g.in.Config().IntervalMinutes
		if mins < 1 {
			mins = config.BurnGuardIntervalMinutesDefault
		}
		t := time.NewTimer(time.Duration(mins) * time.Minute)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-g.refitWake:
			t.Stop()
		case <-t.C:
		}
	}
}

// OnReading tells the runner a usage reading was added. When refit_on_reading is on it
// wakes the runner, which refits the cost model and ticks. It never blocks.
func (g *BurnGuard) OnReading() {
	if g == nil || !g.in.Config().RefitOnReading {
		return
	}
	select {
	case g.refitWake <- struct{}{}:
	default:
	}
}

// Status returns a copy of the state, the last projection and the last decision.
func (g *BurnGuard) Status() BurnGuardStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.status
	st := g.ladder.State()
	s.Applied = st.Applied
	s.Level = len(st.Applied)
	s.Rung = ""
	if n := len(st.Applied); n > 0 {
		s.Rung = st.Applied[n-1]
	}
	s.Line, s.Margin = nil, nil
	if p := s.LastProjection; p != nil && p.Needs == "" {
		line, margin := p.Line, p.Margin
		s.Line, s.Margin = &line, &margin
	}
	if !st.LastChange.IsZero() {
		t := st.LastChange
		s.LastChangeAt = &t
	}
	return s
}

// Tick runs one evaluation and applies its decision. It returns the decision.
func (g *BurnGuard) Tick(ctx context.Context) burnguard.Decision {
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	now := g.in.Now()
	cfg := g.in.Config()
	// Publish the effort ceiling and the Claude retry limit to the executor.
	autonomous.SetBurnGuardLimits(&autonomous.BurnGuardLimits{
		Enabled: cfg.Enabled, EffortCeiling: cfg.EffortCeiling, RetryLimitClaude: cfg.RetryLimitClaude,
	})
	set := burnguard.LadderSettings{
		Enabled: cfg.Enabled, Ladder: cfg.Ladder, StepBack: cfg.StepBack, MarginPoints: cfg.MarginPoints,
		MaxProjectionGapPoints: cfg.MaxProjectionGapPoints, AskOnChange: cfg.AskOnChange,
	}

	if g.in.Applies != nil && !g.in.Applies(burnguard.ClaudeBackend) {
		// The entry in use is api or local with no dollar budget: nothing to guard.
		cfg.Enabled = false
		set.Enabled = false
	}
	var proj burnguard.Projection
	if cfg.Enabled {
		proj = g.project(ctx, cfg, now)
	}
	in := burnguard.TickInput{
		Projection: proj, Settings: set, Now: now,
		InflightCap: g.adm.InflightCap(burnguard.ClaudeBackend),
	}
	oldRung := currentRung(g.ladder.State().Applied)
	d, saveErr := g.ladder.Tick(in)
	if saveErr != nil {
		g.fail("ladder state not saved: %v", saveErr)
	}
	applyErr := g.apply(d)
	if applyErr != nil {
		g.fail("%v", applyErr)
	}
	// A rung that stays applied keeps holding: when the hold rung is still in
	// force and nothing changed, renew the back-off flag it sets so the hold
	// lasts for the next reading. A changed tick already (re)set the flags it
	// carries through its actions above, and a step back or release clears
	// them, so only an unchanged hold is renewed here.
	if !d.Changed && g.holdApplied(d.Applied) {
		if err := g.adm.HoldNewWork(burnguard.ClaudeBackend, true); err != nil {
			g.fail("renew hold: %v", err)
		}
	}
	if d.Changed {
		g.announce(d)
		g.mu.Lock()
		sink := g.rungSink
		g.mu.Unlock()
		if sink != nil {
			sink(oldRung, currentRung(d.Applied))
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.status.Enabled = cfg.Enabled
	g.status.Coefficients = g.fitter.Coefficients()
	t := now
	g.status.LastTickAt = &t
	g.status.Counters.Ticks++
	if cfg.Enabled {
		p := proj
		g.status.LastProjection = &p
	}
	dc := d
	g.status.LastDecision = &dc
	switch d.Direction {
	case burnguard.DirectionClimb:
		g.status.Counters.Climbs++
	case burnguard.DirectionStepBack:
		g.status.Counters.StepBacks++
	case burnguard.DirectionHold:
		g.status.Counters.Holds++
	}
	switch {
	case !cfg.Enabled && d.Level == 0:
		g.status.State = "off"
	case d.Level > 0:
		g.status.State = "guarding"
	case proj.Needs != "":
		g.status.State = "waiting"
	default:
		g.status.State = "idle"
	}
	if applyErr == nil && saveErr == nil {
		g.status.LastError = ""
	}
	return d
}

// project reads the data and builds the projection. A failure is recorded and returned as a
// projection that needs a reading, so the ladder holds.
func (g *BurnGuard) project(ctx context.Context, cfg config.BurnGuardConfig, now time.Time) burnguard.Projection {
	readings, extra, reset, err := g.in.Readings(ctx)
	if err != nil {
		g.fail("usage readings: %v", err)
		return burnguard.Projection{Needs: "readings unavailable"}
	}
	var buckets []burnguard.Bucket
	var through time.Time
	if g.in.TranscriptDir != "" {
		u, err := burnguard.ReadDir(g.in.TranscriptDir, burnguard.ReadOptions{Since: now.Add(-8 * 24 * time.Hour)})
		if err != nil {
			g.fail("transcripts: %v", err)
		} else {
			buckets, through = u.Buckets, u.Through
		}
	}
	if cfg.RefitOnReading {
		if res, did := g.fitter.Observe(buckets, readings, through); did {
			g.mu.Lock()
			g.status.Counters.Refits++
			r := res
			g.status.LastFit = &r
			g.mu.Unlock()
		}
	}
	var margin *float64
	if g.in.MarginPoints != nil {
		margin = g.in.MarginPoints()
	}
	return burnguard.Project(burnguard.ProjectInput{
		Readings: readings, Reset: reset, Now: now,
		Buckets: buckets, Through: through, Coef: g.fitter.Coefficients(),
		MarginPoints: margin, ExtraCreditPoints: extra,
	})
}

// apply carries out a decision's actions on the admission seam. It never touches a session.
func (g *BurnGuard) apply(d burnguard.Decision) error {
	var errs []error
	for _, a := range d.Actions {
		var err error
		switch a.Kind {
		case burnguard.ActionSetInflight:
			err = g.adm.SetInflight(a.Backend, a.Value)
		case burnguard.ActionRouteBackend:
			err = g.adm.RouteBackend(a.Backend, a.To, !a.Release)
		case burnguard.ActionHoldNewWork:
			err = g.adm.HoldNewWork(a.Backend, !a.Release)
		default:
			err = fmt.Errorf("unknown action kind %q", a.Kind)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", a.Kind, a.Backend, err))
			continue
		}
		g.mu.Lock()
		g.status.Counters.ActionsDone++
		g.mu.Unlock()
	}
	return errors.Join(errs...)
}

// announce raises the alert and the queue question of a step. The text carries the numbers
// and what was done, and says that running sessions were left alone.
func (g *BurnGuard) announce(d burnguard.Decision) {
	if g.notify == nil {
		return
	}
	running := ""
	if g.sessions != nil {
		running = fmt.Sprintf(" %d running session(s) were left alone.", g.sessions.RunningCount())
	}
	title := "Burn guard: " + string(d.Direction) + " " + d.Rung
	g.notify.Alert(alerts.LevelWarn, title, d.AlertText+running)
	g.mu.Lock()
	store, ask := g.questionStore, g.in.Config().AskOnChange
	g.mu.Unlock()
	if store != nil {
		// the ask_on_change wiring: it withdraws the guard's open question
		// before raising the new one, or, when ask_on_change is off, withdraws
		// the open guard question instead of raising
		autonomous.ApplyBurnGuardAskOnChange(store, ask, d, running)
		return
	}
	if d.QuestionText == "" {
		return
	}
	if err := g.notify.Question(d.QuestionText + running); err != nil {
		g.fail("queue question not raised: %v", err)
	}
}

// holdApplied reports whether the hold rung (hold_claude) is in force after the decision.
func (g *BurnGuard) holdApplied(applied []string) bool {
	for _, rung := range applied {
		if rung == burnguard.RungHoldClaude {
			return true
		}
	}
	return false
}

func (g *BurnGuard) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[burn-guard] %s", msg)
	g.mu.Lock()
	g.status.LastError = msg
	g.status.Counters.Errors++
	g.mu.Unlock()
}

// BurnGuardStats is the observability block (SystemStats and Prometheus).
type BurnGuardStats struct {
	State          string
	Level          int
	Projected      float64
	Target         float64
	OverBy         float64
	PointsPerHour  float64
	LastTickAgeSec int64
	Counters       BurnGuardCounters
}

// Stats summarises Status for SystemStats and the Prometheus collector. It is zero when no
// tick has run.
func (g *BurnGuard) Stats() BurnGuardStats {
	if g == nil {
		return BurnGuardStats{State: "off"}
	}
	s := g.Status()
	out := BurnGuardStats{State: s.State, Level: s.Level, Counters: s.Counters, LastTickAgeSec: -1}
	if s.LastProjection != nil {
		out.Projected, out.Target = s.LastProjection.Projected, s.LastProjection.Target
		out.OverBy, out.PointsPerHour = s.LastProjection.OverBy, s.LastProjection.PointsPerHour
	}
	if s.LastTickAt != nil {
		out.LastTickAgeSec = int64(g.in.Now().Sub(*s.LastTickAt).Seconds())
	}
	return out
}

// burnGuardFileStore saves the ladder state as one JSON file.
type burnGuardFileStore struct{ path string }

func (f burnGuardFileStore) Load() (burnguard.LadderState, error) {
	var st burnguard.LadderState
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

func (f burnGuardFileStore) Save(st burnguard.LadderState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// burnGuardNotifier raises alerts on the alert store and questions on the question store.
type burnGuardNotifier struct{ s *Server }

func (n burnGuardNotifier) Alert(level alerts.Level, title, body string) {
	if n.s == nil || n.s.alertStore == nil {
		return
	}
	n.s.alertStore.AddSystem(level, title, body)
}

func (n burnGuardNotifier) Question(text string) error {
	if n.s == nil || n.s.questionStore == nil {
		return errors.New("no question store wired")
	}
	_, err := n.s.questionStore.Raise(autonomous.Question{
		AskedBy:   "burn-guard",
		AskerKind: autonomous.QuestionAskerSystem,
		Context:   autonomous.QuestionContextNone,
		Text:      text,
		Options: []autonomous.QuestionOption{
			{ID: "keep", Label: "Leave it in place", Tradeoffs: "Admission stays restricted until the projection is back under the line."},
			{ID: "release", Label: "Release it", Tradeoffs: "New work is admitted as before; the weekly burn may run over."},
		},
		Default: "keep",
		Risk:    autonomous.QuestionRiskLow,
		Source:  autonomous.QuestionSourceRest,
	})
	return err
}

// AttachBurnGuard builds the runner from the daemon's live config, the shared readings
// series and the Claude transcript directory, and sets it on the server. It does not start
// it; call StartBurnGuard. adm is the admission seam; sessions may be nil.
func (s *Server) AttachBurnGuard(adm BurnGuardAdmission, sessions BurnGuardSessions, transcriptDir string) error {
	in := BurnGuardInputs{
		TranscriptDir: transcriptDir,
		Applies:       s.burnGuardApplies,
		Config: func() config.BurnGuardConfig {
			if s.cfg == nil {
				return config.DefaultBurnGuard()
			}
			return s.cfg.Usage.BurnGuard
		},
		Readings: func(ctx context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			series, err := s.usageSeries()
			if err != nil {
				return nil, 0, usagecalc.ResetSpec{}, err
			}
			snap, err := series.Snapshot(ctx)
			if err != nil {
				return nil, 0, usagecalc.ResetSpec{}, err
			}
			settings := UsageCalculatorSettings(config.UsageConfig{})
			if s.cfg != nil {
				settings = UsageCalculatorSettings(s.cfg.Usage)
			}
			return snap.Record.Readings, snap.Record.ExtraCreditPoints, settings.Reset, nil
		},
		MarginPoints: func() *float64 {
			if s.cfg == nil {
				return nil
			}
			return UsageCalculatorSettings(s.cfg.Usage).SafetyPoints
		},
	}
	g, err := NewBurnGuard(in, adm, sessions, burnGuardNotifier{s: s},
		burnGuardFileStore{path: filepath.Join(s.usageDataDir(), "burn_guard_state.json")})
	if err != nil {
		return err
	}
	if s.questionStore != nil {
		g.AttachQuestionStore(s.questionStore)
	}
	if rn, ok := s.autonomousMgr.(interface{ NoteBurnGuardRung(oldRung, newRung string) }); ok {
		g.AttachRungSink(rn.NoteBurnGuardRung)
	}
	s.burnGuard = g
	return nil
}

// handleBurnGuardStatus serves GET /api/usage/burn-guard (usage:read): the guard's state, last
// projection and last decision, for the PWA, the CLI and the other surfaces. When the guard
// is not wired it answers {"enabled":false,"state":"off"}.
func (s *Server) handleBurnGuardStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, usageCapabilityRead) {
		return
	}
	st := BurnGuardStatus{State: "off"}
	if g := s.burnGuard; g != nil {
		st = g.Status()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

// BurnGuard returns the runner, or nil until AttachBurnGuard has run.
func (s *Server) BurnGuard() *BurnGuard { return s.burnGuard }

// StartBurnGuard runs the guard until ctx is cancelled. It does nothing when the guard is
// not attached.
func (s *Server) StartBurnGuard(ctx context.Context) {
	if g := s.burnGuard; g != nil {
		// at start: open guard questions a previous run raised for a state this
		// one no longer requires are pruned now; a question the loaded state
		// still applies is kept
		if s.questionStore != nil {
			g.AttachQuestionStore(s.questionStore)
			autonomous.PruneBurnGuardQuestionsAtStart(s.questionStore, g.LadderState())
		}
		go g.Run(ctx)
	}
}

// AttachBurnGuard wires the guard on the embedded API server.
func (s *HTTPServer) AttachBurnGuard(adm BurnGuardAdmission, sessions BurnGuardSessions, transcriptDir string) error {
	if s.api == nil {
		return fmt.Errorf("burn guard: no API server to wire")
	}
	return s.api.AttachBurnGuard(adm, sessions, transcriptDir)
}

// StartBurnGuard starts the guard on the embedded API server.
func (s *HTTPServer) StartBurnGuard(ctx context.Context) {
	if s.api != nil {
		s.api.StartBurnGuard(ctx)
	}
}

// BurnGuardStats returns the guard's stats for the collector; a zero value when unwired.
func (s *HTTPServer) BurnGuardStats() BurnGuardStats {
	if s.api == nil {
		return BurnGuardStats{State: "off"}
	}
	return s.api.burnGuard.Stats()
}

// BurnGuardStatus returns the guard's status for the surfaces.
func (s *HTTPServer) BurnGuardStatus() (BurnGuardStatus, bool) {
	if s.api == nil || s.api.burnGuard == nil {
		return BurnGuardStatus{}, false
	}
	return s.api.burnGuard.Status(), true
}
