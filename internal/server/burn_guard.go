// burn_guard.go — the burn guard runner. Every usage.burn_guard.interval_minutes it
// evaluates each paid LLM entry once: it reads the entry's own usage readings, prices its
// tokens at the entry's model's own cost rate, projects the burn against the entry's
// target pace line, asks the ladder (internal/burnguard) for a Decision and applies it:
//
//	lower_inflight   lower the entry's inflight cap by one (the original is remembered
//	                 and restored on release)
//	route_pi_prompt  route the entry's new work to its route target
//	route_cheaper_model  route this entry's new work to its own cheaper model (same backend)
//	hold_entry       hold the entry's new work (on its held models when it lists any)
//
// The first version of the guard named the hold rungs route_pi_prompt and hold_claude.
// Those names are still accepted in a saved config and normalize to the entry-generic
// names: the guard reads every rung from the entry, and the back-off flag of a hold
// carries the rung name the ladder recorded it under.
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
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
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
	// rung is the name the ladder recorded for the hold rung it acts on: the guard
	// records it on the flag it sets, so the rung shown on the flag is the rung in
	// force, whatever the ladder calls it (entry-generic or legacy first-version name).
	HoldNewWork(backend, rung string, on bool) error
}

// BurnGuardModelHold is the optional seam that holds new work on some models of a
// backend only (an entry's held models). Without it a hold covers the whole backend.
// rung is the rung name the guard records on the flag it sets (see HoldNewWork).
type BurnGuardModelHold interface {
	HoldModels(backend string, models []string, rung string, on bool) error
}

// BurnGuardModelRoute is the optional seam that the route_cheaper_model rung drives:
// it routes (on) new work meant for backend to a cheaper model of that same backend,
// and stops routing (off) to restore the entry's original model routing. It touches
// only admission of new work; a running session that already picked a model keeps it.
type BurnGuardModelRoute interface {
	RouteModel(backend, model string, on bool) error
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
	// Tokens returns an entry's hourly token buckets since the given time and how far
	// the data goes; first is true for the first entry. nil means the transcript
	// directory for the first entry and no tokens for the others.
	Tokens func(ctx context.Context, entry string, first bool, since time.Time) ([]burnguard.Bucket, time.Time, error)
	// Rate returns the entry's model's own cost rate (the per-model rate of the LLM
	// entry configuration); false when none is configured. nil means no rates.
	Rate func(entry config.BurnGuardEntryConfig) (burnguard.ModelRate, bool)
	// CheaperModel returns the model the entry's route_cheaper_model rung should send
	// new work to: the entry's own cheaper model (for a Claude entry, Haiku). The
	// entry's explicit route_cheaper_model setting wins; otherwise the entry's own
	// cheapest priced model by its per-model cost rates. "" means the rung is a no-op
	// for that entry. nil means no cheaper-model resolution (the rung is a no-op).
	CheaperModel func(entry config.BurnGuardEntryConfig) string
	// StoreFor returns where an entry other than the first saves its ladder state; nil
	// means no persistence for those entries.
	StoreFor func(entry string) burnguard.StateStore
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Applies reports whether the guard acts on the backend: true for an allowance
	// entry, and for an api or local entry only with a dollar budget set. nil means
	// it always applies (the behaviour before billing modes).
	Applies func(backend string) bool
	// PaceLogPath is the JSON-lines file the pace policy appends its decisions to and
	// reads the recent ones back from at start; empty keeps them in memory only.
	PaceLogPath string
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
	// Entries is one status per guarded entry, the first entry first. The fields
	// above repeat the first entry's.
	Entries []BurnGuardEntryStatus `json:"entries,omitempty"`
	// Pace is the pace policy: its mode, the hold it keeps and the decisions it recorded
	// with their evidence.
	Pace *BurnGuardPaceStatus `json:"pace,omitempty"`
}

// BurnGuardEntryStatus is one guarded entry's state.
type BurnGuardEntryStatus struct {
	LLM            string                `json:"llm"`
	Plan           string                `json:"plan"`
	HasUsageSource bool                  `json:"has_usage_source"`
	Enabled        bool                  `json:"enabled"`
	State          string                `json:"state"`
	Level          int                   `json:"level"`
	Rung           string                `json:"rung,omitempty"`
	Applied        []string              `json:"applied,omitempty"`
	HeldModels     []string              `json:"held_models,omitempty"`
	RouteTo        string                `json:"route_to,omitempty"`
	CheaperModel   string                `json:"cheaper_model,omitempty"`
	LastProjection *burnguard.Projection `json:"last_projection,omitempty"`
	LastDecision   *burnguard.Decision   `json:"last_decision,omitempty"`
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
	entries       map[string]*guardedEntry // entries other than the first, by name
	rungSink      func(oldRung, newRung string)
	pace          map[string]*paceState // the pace policy's state by entry name
	paceOrder     []string
	paceLog       []burnguard.PaceDecision // newest first

	estMu sync.Mutex // guards est
	est   estimateTokens
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
	g.paceLog = loadPaceLog(in.PaceLogPath)
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
	s.Pace = g.paceStatusLocked()
	return s
}

// Tick evaluates every guarded entry once and applies each entry's decision. It returns
// the first entry's decision (the entry the guard acted on before per-entry guarding).
func (g *BurnGuard) Tick(ctx context.Context) burnguard.Decision {
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	now := g.in.Now()
	cfg := g.in.Config()
	// Publish the effort ceiling and the Claude retry limit to the executor.
	autonomous.SetBurnGuardLimits(&autonomous.BurnGuardLimits{
		Enabled: cfg.Enabled, EffortCeiling: cfg.EffortCeiling, RetryLimitClaude: cfg.RetryLimitClaude,
	})
	entries := cfg.EffectiveEntries()
	usagecalc.SetDefaultReadingEntry(entries[0].LLM)

	// The readings are read once for every entry; each entry keeps its own.
	var all []usagecalc.Reading
	var extra float64
	var reset usagecalc.ResetSpec
	var readErr error
	readingsRead := false
	readings := func() ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
		if !readingsRead {
			readingsRead = true
			all, extra, reset, readErr = g.in.Readings(ctx)
		}
		return all, extra, reset, readErr
	}

	var first burnguard.Decision
	var statuses []BurnGuardEntryStatus
	for i, e := range entries {
		es := g.entryState(e, i == 0)
		d, proj, enabled := g.tickEntry(ctx, cfg, e, i == 0, len(entries) > 1, es, now, readings)
		st := BurnGuardEntryStatus{
			LLM: e.LLM, Plan: e.Plan, HasUsageSource: e.HasUsageSource(), Enabled: enabled,
			Level: d.Level, Applied: d.Applied, HeldModels: e.HeldModels, RouteTo: e.RouteTo,
			CheaperModel: g.entryCheaperModel(e),
			Rung:         currentRung(d.Applied), State: entryStateName(enabled, d, proj),
		}
		if enabled {
			p := proj
			st.LastProjection = &p
		}
		dc := d
		st.LastDecision = &dc
		statuses = append(statuses, st)
		if i == 0 {
			first = d
			g.recordFirst(cfg, enabled, proj, d, now)
		}
	}
	g.mu.Lock()
	g.status.Entries = statuses
	g.mu.Unlock()
	return first
}

// entryStateName is the State of one entry, with the meanings of BurnGuardStatus.State.
func entryStateName(enabled bool, d burnguard.Decision, proj burnguard.Projection) string {
	switch {
	case !enabled && d.Level == 0:
		return "off"
	case d.Level > 0:
		return "guarding"
	case proj.Needs != "":
		return "waiting"
	default:
		return "idle"
	}
}

// guardedEntry is the runner's state for one entry: its ladder and its cost model.
type guardedEntry struct {
	ladder *burnguard.Ladder
	fitter *burnguard.Fitter
	// buckets, through and coef are the token data and prices of the entry's last
	// projection, which the pace policy prices the burn since a reading from.
	buckets []burnguard.Bucket
	through time.Time
	coef    burnguard.Coefficients
}

// entryState returns (building on first use) the state of entry e. The first entry uses
// the ladder and fitter the runner was built with, so its saved state is the one saved
// before per-entry guarding.
// entryCheaperModel resolves the model the entry's route_cheaper_model rung applies: the
// entry's explicit route_cheaper_model setting else the entry's cheaper model resolved
// from its per-model cost rates (nil resolver → ""), so the displayed value is exactly
// the one the ladder routes to. "" means the rung is a no-op for this entry.
func (g *BurnGuard) entryCheaperModel(e config.BurnGuardEntryConfig) string {
	if e.RouteCheaperModel != "" {
		return e.RouteCheaperModel
	}
	if g.in.CheaperModel != nil {
		return g.in.CheaperModel(e)
	}
	return ""
}

func (g *BurnGuard) entryState(e config.BurnGuardEntryConfig, first bool) *guardedEntry {
	target := burnguard.EntryTarget{Backend: e.LLM, RouteTo: e.RouteTo, CheaperModel: g.entryCheaperModel(e), HeldModels: append([]string(nil), e.HeldModels...)}
	g.mu.Lock()
	defer g.mu.Unlock()
	if first {
		g.ladder.SetTarget(target)
		return &guardedEntry{ladder: g.ladder, fitter: g.fitter}
	}
	if es, ok := g.entries[e.LLM]; ok {
		es.ladder.SetTarget(target)
		return es
	}
	var store burnguard.StateStore
	if g.in.StoreFor != nil {
		store = g.in.StoreFor(e.LLM)
	}
	ladder, err := burnguard.NewEntryLadder(store, target)
	if err != nil {
		log.Printf("[burn-guard] %s: %v; starting with no rung in force", e.LLM, err)
		ladder, _ = burnguard.NewEntryLadder(nil, target)
	}
	cfg := g.in.Config()
	es := &guardedEntry{ladder: ladder, fitter: burnguard.NewFitter(burnguard.Coefficients{
		CacheWritePerM: cfg.CacheWritePointsPerMillion, CacheReadPerM: cfg.CacheReadPointsPerMillion,
	})}
	if g.entries == nil {
		g.entries = map[string]*guardedEntry{}
	}
	g.entries[e.LLM] = es
	return es
}

// tickEntry runs one entry's evaluation and applies its decision. An entry with no usage
// source, or one the billing mode says is not guarded, is evaluated as a disabled ladder:
// it is never held, and anything left in force is released.
func (g *BurnGuard) tickEntry(ctx context.Context, cfg config.BurnGuardConfig, e config.BurnGuardEntryConfig, first, many bool, es *guardedEntry, now time.Time,
	readings func() ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error)) (burnguard.Decision, burnguard.Projection, bool) {
	ladderRungs := cfg.Ladder
	if len(e.Ladder) > 0 {
		ladderRungs = e.Ladder
	}
	enabled := cfg.Enabled && e.HasUsageSource()
	if enabled && g.in.Applies != nil && !g.in.Applies(e.LLM) {
		// The entry is api or local with no dollar budget: nothing to guard.
		enabled = false
	}
	set := burnguard.LadderSettings{
		Enabled: enabled, Ladder: ladderRungs, StepBack: cfg.StepBack, MarginPoints: cfg.MarginPoints,
		MaxProjectionGapPoints: cfg.MaxProjectionGapPoints, AskOnChange: cfg.AskOnChange,
	}
	var proj burnguard.Projection
	if enabled {
		proj = g.project(ctx, cfg, e, first, es, now, readings)
	}
	in := burnguard.TickInput{Projection: proj, Settings: set, Now: now, InflightCap: g.adm.InflightCap(e.LLM)}
	oldRung := currentRung(es.ladder.State().Applied)
	d, saveErr := es.ladder.Tick(in)
	if saveErr != nil {
		g.fail("%s: ladder state not saved: %v", e.LLM, saveErr)
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
		if err := g.hold(burnguard.Action{Backend: e.LLM, Models: e.HeldModels, Rung: currentRung(d.Applied)}, true); err != nil {
			g.fail("renew hold: %v", err)
		}
	}
	if d.Changed {
		label := ""
		if many {
			label = e.LLM
		}
		g.announce(d, label)
		if first {
			g.mu.Lock()
			sink := g.rungSink
			g.mu.Unlock()
			if sink != nil {
				sink(oldRung, currentRung(d.Applied))
			}
		}
	}
	g.mu.Lock()
	switch d.Direction {
	case burnguard.DirectionClimb:
		g.status.Counters.Climbs++
	case burnguard.DirectionStepBack:
		g.status.Counters.StepBacks++
	case burnguard.DirectionHold:
		g.status.Counters.Holds++
	}
	if applyErr == nil && saveErr == nil && first {
		g.status.LastError = ""
	}
	g.mu.Unlock()
	// The pace policy runs last, so an error it reports is not cleared by the ladder's own
	// clean tick.
	g.tickPace(cfg, e, first, enabled, es, proj, now, readings)
	return d, proj, enabled
}

// recordFirst copies the first entry's result onto the top-level status fields.
func (g *BurnGuard) recordFirst(cfg config.BurnGuardConfig, enabled bool, proj burnguard.Projection, d burnguard.Decision, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status.Enabled = enabled
	g.status.Coefficients = g.fitter.Coefficients()
	t := now
	g.status.LastTickAt = &t
	g.status.Counters.Ticks++
	if enabled {
		p := proj
		g.status.LastProjection = &p
	}
	dc := d
	g.status.LastDecision = &dc
	g.status.State = entryStateName(enabled, d, proj)
}

// project reads one entry's data and builds its projection. A failure is recorded and
// returned as a projection that needs a reading, so the ladder holds.
func (g *BurnGuard) project(ctx context.Context, cfg config.BurnGuardConfig, e config.BurnGuardEntryConfig, first bool, es *guardedEntry, now time.Time,
	read func() ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error)) burnguard.Projection {
	all, extra, reset, err := read()
	if err != nil {
		g.fail("usage readings: %v", err)
		return burnguard.Projection{Needs: "readings unavailable"}
	}
	readings := usagecalc.ReadingsForEntry(all, e.LLM, first)
	buckets, through := g.tokens(ctx, e, first, tokensSince(readings, reset, now))
	if first {
		g.keepEstimateData(e.LLM, now, buckets, through)
	}
	rate, hasRate := burnguard.ModelRate{}, false
	if g.in.Rate != nil {
		rate, hasRate = g.in.Rate(e)
		hasRate = hasRate && !rate.IsZero()
	}
	var coef burnguard.Coefficients
	switch {
	case e.IsMetered():
		// A metered entry projects from spend: its tokens are priced in dollars at the
		// model's own rate (none: the spend readings alone carry the burn).
		if hasRate {
			coef = burnguard.CoefficientsFromRate(rate, 1)
		}
	case hasRate && e.PointsPerUSD > 0:
		coef = burnguard.CoefficientsFromRate(rate, e.PointsPerUSD)
	default:
		if cfg.RefitOnReading {
			if res, did := es.fitter.Observe(buckets, readings, through); did {
				g.mu.Lock()
				g.status.Counters.Refits++
				if first {
					r := res
					g.status.LastFit = &r
				}
				g.mu.Unlock()
			}
		}
		coef = es.fitter.Coefficients()
	}
	var margin *float64
	if g.in.MarginPoints != nil {
		margin = g.in.MarginPoints()
	}
	if !first {
		// Bought extra credit is the account reading's (the first entry's).
		extra = 0
	}
	es.buckets, es.through, es.coef = buckets, through, coef
	return burnguard.ProjectEntry(burnguard.EntryProjectInput{
		ProjectInput: burnguard.ProjectInput{
			Readings: readings, Reset: reset, Now: now,
			Buckets: buckets, Through: through, Coef: coef,
			MarginPoints: margin, ExtraCreditPoints: extra,
		},
		Metered: e.IsMetered(), PeriodTarget: e.PeriodTarget,
	})
}

// tokens reads one entry's token buckets: from the Tokens source when one is wired, else
// the transcript directory for the first entry only.
func (g *BurnGuard) tokens(ctx context.Context, e config.BurnGuardEntryConfig, first bool, since time.Time) ([]burnguard.Bucket, time.Time) {
	if g.in.Tokens != nil {
		b, through, err := g.in.Tokens(ctx, e.LLM, first, since)
		if err != nil {
			g.fail("%s tokens: %v", e.LLM, err)
			return nil, time.Time{}
		}
		return b, through
	}
	if !first || g.in.TranscriptDir == "" {
		return nil, time.Time{}
	}
	u, err := burnguard.ReadDir(g.in.TranscriptDir, burnguard.ReadOptions{Since: since})
	if err != nil {
		g.fail("transcripts: %v", err)
		return nil, time.Time{}
	}
	return u.Buckets, u.Through
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
		case burnguard.ActionRouteModel:
			err = g.routeModel(a, !a.Release)
		case burnguard.ActionHoldNewWork:
			err = g.hold(a, !a.Release)
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

// hold holds or releases new work on backend: on its held models only when the entry
// lists some and the admission seam can hold by model, else the whole backend. It hands
// the admission seam the rung name the ladder recorded the action under, so the flag it
// sets shows the rung in force rather than an assumed one.
func (g *BurnGuard) hold(a burnguard.Action, on bool) error {
	if mh, ok := g.adm.(BurnGuardModelHold); ok && len(a.Models) > 0 {
		return mh.HoldModels(a.Backend, a.Models, a.Rung, on)
	}
	return g.adm.HoldNewWork(a.Backend, a.Rung, on)
}

// routeModel routes (on) or stops routing (off) new work meant for the entry's backend
// to its cheaper model. It drives the optional BurnGuardModelRoute seam; without it the
// rung has nowhere to take effect and the apply is reported as an error (fail safe).
func (g *BurnGuard) routeModel(a burnguard.Action, on bool) error {
	if mr, ok := g.adm.(BurnGuardModelRoute); ok {
		return mr.RouteModel(a.Backend, a.Model, on)
	}
	return fmt.Errorf("route_cheaper_model: no model-route seam wired on the admission seam")
}

// announce raises the alert and the queue question of a step. The text carries the numbers
// and what was done, and says that running sessions were left alone.
func (g *BurnGuard) announce(d burnguard.Decision, entry string) {
	if g.notify == nil {
		return
	}
	running := ""
	if g.sessions != nil {
		running = fmt.Sprintf(" %d running session(s) were left alone.", g.sessions.RunningCount())
	}
	title := "Burn guard: " + string(d.Direction) + " " + d.Rung
	if entry != "" {
		title += " (" + entry + ")"
	}
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

// holdApplied reports whether the hold rung is in force after the decision, under either
// naming: the entry-generic name or the legacy first-version name.
func (g *BurnGuard) holdApplied(applied []string) bool {
	for _, rung := range applied {
		if burnguard.IsHoldRung(rung) {
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
	in.Rate = s.burnGuardEntryRate
	in.CheaperModel = s.burnGuardEntryCheaperModel
	in.StoreFor = func(entry string) burnguard.StateStore {
		return burnGuardFileStore{path: filepath.Join(s.usageDataDir(), "burn_guard_state."+safeEntryFileName(entry)+".json")}
	}
	in.PaceLogPath = filepath.Join(s.usageDataDir(), "burn_guard_pace.jsonl")
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

// burnGuardEntryRate is the entry's model's own cost rate: the per-model rate on the LLM
// entry, falling back field by field to the backend's cost_rates (built-in defaults
// merged under the configured ones).
func (s *Server) burnGuardEntryRate(e config.BurnGuardEntryConfig) (burnguard.ModelRate, bool) {
	l, ok := s.llmEntry(e.LLM)
	if !ok {
		return burnguard.ModelRate{}, false
	}
	rates := map[string]config.CostRateConfig{}
	for k, v := range session.DefaultCostRates() {
		rates[k] = config.CostRateConfig{InPerK: v.InPerK, OutPerK: v.OutPerK}
	}
	if s.cfg != nil {
		for k, v := range s.cfg.Session.CostRates {
			rates[k] = v
		}
	}
	// The entry name is the backend name (as admission resolves it).
	r := config.ResolveModelCostRate(l, e.Model, l.Name, rates)
	if r.Source == "" {
		return burnguard.ModelRate{}, false
	}
	return burnguard.ModelRate{InPerK: r.InPerK, OutPerK: r.OutPerK, CacheReadPerK: r.CacheReadPerK, CacheWritePerK: r.CacheWritePerK}, true
}

// burnGuardEntryCheaperModel is the entry's cheaper model for the route_cheaper_model
// rung: the entry's own cheapest priced other model by its per-model cost rates (the
// per-model rate of the LLM entry, backend cost_rates merged in), preferring a Haiku
// model for a Claude entry. It returns the model name, or "" when there is no other
// model to route to (the rung is a no-op). It does not read the entry's explicit
// route_cheaper_model setting; entryState prefers that one when it is set.
func (s *Server) burnGuardEntryCheaperModel(e config.BurnGuardEntryConfig) string {
	l, ok := s.llmEntry(e.LLM)
	if !ok || len(l.Models) == 0 {
		return ""
	}
	rates := map[string]config.CostRateConfig{}
	for k, v := range session.DefaultCostRates() {
		rates[k] = config.CostRateConfig{InPerK: v.InPerK, OutPerK: v.OutPerK}
	}
	if s.cfg != nil {
		for k, v := range s.cfg.Session.CostRates {
			rates[k] = v
		}
	}
	backend := l.Name
	if backend == "" {
		backend = e.LLM
	}
	var cands []config.EntryModelCost
	for i := range l.Models {
		em := l.Models[i]
		if em.Model == "" || em.Model == e.Model {
			continue
		}
		r := config.ResolveModelCostRate(l, em.Model, backend, rates)
		cands = append(cands, config.EntryModelCost{Model: em.Model, InPerK: r.InPerK, OutPerK: r.OutPerK})
	}
	prefer := l.Kind == inference.KindClaude || l.Kind == inference.KindClaudeCode ||
		strings.Contains(strings.ToLower(l.Name), "claude") ||
		strings.Contains(strings.ToLower(e.Model), "claude")
	return config.PickCheaperModel(e.Model, prefer, cands)
}

// safeEntryFileName keeps letters, digits, dot, dash and underscore of an entry name.
func safeEntryFileName(name string) string {
	b := []byte(name)
	for i, c := range b {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			b[i] = '_'
		}
	}
	return string(b)
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
