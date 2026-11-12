// usage_autolanes_wiring.go — the daemon-side hourly auto-lanes loop
// (usage.calculator.auto_lanes).
//
// The usage calculator's advice suggests a lane count for the active policy,
// and the pure rule DecideAutoLanes turns that into a concrete in-flight cap
// for this daemon: the advice lanes plus the open Automata whose capacity
// priority is at or above usage.calculator.auto_lanes_special_priority,
// clamped to at most usage.calculator.auto_lanes_max and to at least 1. When
// usage.calculator.auto_lanes is true, this loop runs once at daemon start and
// once every hour after: it reads the freshest advice through the same
// service the REST surface serves, counts the special items, applies the
// rule, and — only when the target differs from the current cap — sets the
// first enabled claude-code LLM entry's max_inflight through the same
// registry save the operator's LLM update uses, so the capacity ledger picks
// it up live. Every decision, moved or not, is appended to the auto-lanes
// decision log (usagecalc.OpenAutoLanesLog) in the data dir. When
// usage.calculator.auto_lanes is false the loop does nothing: lane changes
// stay with the operator.
package main

import (
	"context"
	"errors"
	"log"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// autoLanesAdvisor is what one hourly pass consumes from the usage calculator
// advice service — satisfied by *usagecalc.Service, and by the test fakes.
type autoLanesAdvisor interface {
	Advice(ctx context.Context, opts usagecalc.Options) (*usagecalc.AdviceResult, error)
}

// autoLanesWiring is one hourly pass of the loop with every collaborator
// injected, so the wiring is testable without a daemon: the advisor (fake
// usage service), the target LLM entry plus the save path (fake or real
// registry), the special-item count from the autonomous Manager, the live
// settings, the decision log, the clock, and the log sink.
type autoLanesWiring struct {
	advisor  autoLanesAdvisor
	target   func() (*inference.LLM, bool) // first enabled claude-code LLM entry
	apply    func(l *inference.LLM) error  // registry save path
	special  func(threshold int) int       // open Automata at or above the threshold
	usage    func() config.UsageCalculatorConfig
	logStore *usagecalc.AutoLanesLog
	now      func() time.Time
	logf     func(format string, args ...any)
}

// note appends one daemon log line; silent when no sink is wired.
func (w *autoLanesWiring) note(format string, args ...any) {
	if w.logf != nil {
		w.logf("[auto-lanes] "+format, args...)
	}
}

// runOnce is one hourly pass: advice → special items → decision → (when the
// target moved) registry save. It never raises the cap above the operator
// maximum — DecideAutoLanes clamps — and it appends the decision to the log
// before it applies anything, so a failed save still leaves the record.
func (w *autoLanesWiring) runOnce(ctx context.Context) {
	if w.advisor == nil {
		return // advice service not attached; nothing to apply
	}
	u := w.usage()
	if !u.AutoLanes {
		return // off: lane changes stay with the operator
	}
	entry, ok := w.target()
	if !ok || entry == nil {
		w.note("skip: no enabled claude-code LLM entry in the registry")
		return
	}
	res, err := w.advisor.Advice(ctx, usagecalc.Options{})
	if err != nil {
		w.note("skip: advice: %v", err)
		return
	}
	if res == nil {
		w.note("skip: empty advice")
		return
	}
	advice := res.Advice

	special := 0
	if w.special != nil {
		special = w.special(u.AutoLanesSpecialPriority)
	}
	current := entry.MaxInflight
	d := usagecalc.DecideAutoLanes(advice.Advice.LanesSuggested, special, u.AutoLanesMax, current)
	w.note("%s %d -> %d: %s", d.Direction, current, d.Target, d.Reason)

	running := 0
	if advice.LanesRunningNow != nil {
		running = *advice.LanesRunningNow
	}
	if w.logStore != nil {
		rec := usagecalc.AutoLanesLogEntry{
			At:           w.now().UTC(),
			From:         current,
			To:           d.Target,
			Direction:    d.Direction,
			Reason:       d.Reason,
			Policy:       res.Policy,
			LanesRunning: running,
			SpecialItems: special,
		}
		if lerr := w.logStore.Append(rec); lerr != nil {
			w.note("decision log: %v", lerr)
		}
	}

	if d.Direction == usagecalc.AutoLanesUnchanged {
		return // cap already correct
	}
	if w.apply == nil {
		w.note("target %d not applied: no save path wired", d.Target)
		return
	}
	l := *entry
	l.MaxInflight = d.Target
	if aerr := w.apply(&l); aerr != nil {
		w.note("apply max_inflight=%d: %v (decision logged, cap unchanged)", d.Target, aerr)
		return
	}
	w.note("applied %s max_inflight=%d on %s", d.Direction, d.Target, entry.Name)
}

// Start runs one pass immediately and one more on every tick. The loop ends
// when ctx is done. The test harness feeds its own channel to fake the clock;
// the production loop uses StartHourly for process-lifetime ticks.
func (w *autoLanesWiring) Start(ctx context.Context, tick <-chan time.Time) {
	w.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			w.runOnce(ctx)
		}
	}
}

// StartHourly runs the loop on a real hour ticker, stopping the ticker when
// ctx is done and the loop has exited. It blocks until ctx is done.
func (w *autoLanesWiring) StartHourly(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	w.Start(ctx, t.C)
}

// startAutoLanesLoop wires the live loop for main and then runs it for the
// lifetime of the daemon's context (so the caller runs it in a goroutine):
// advice from the attached calculator service, the target entry and the save
// path from the LLM registry (the operator's LLM update goes through the
// same Registry.Update), the special-item count from the autonomous
// Manager, the settings read live off the config on every pass, and the
// decision log beside the usage series in the data dir.
func startAutoLanesLoop(ctx context.Context, cfg *config.Config, httpServer *server.HTTPServer, amgr *autonomouspkg.Manager, dataDir string) {
	logStore, lerr := usagecalc.OpenAutoLanesLog(dataDir)
	if lerr != nil {
		log.Printf("[auto-lanes] decision log: %v (decisions stay in memory only)", lerr)
		logStore = nil
	}
	svc := httpServer.UsageCalculator()
	reg := httpServer.InferenceRegistry()
	w := &autoLanesWiring{
		apply: func(l *inference.LLM) error {
			if reg == nil {
				return errAutoLanesNoRegistry
			}
			// Same save as the operator's PUT /api/llms/{name}:
			// Registry.Update validates, replaces the shared entry and
			// persists; the ledger's 10s limit sync picks the new
			// max_inflight up live.
			return reg.Update(l)
		},
		special: func(threshold int) int {
			if amgr == nil {
				return 0
			}
			return amgr.CountOpenPRDsWithPriority(threshold)
		},
		usage: func() config.UsageCalculatorConfig {
			if cfg == nil {
				return config.UsageCalculatorConfig{}
			}
			// Effective, not raw: a runtime config reload that leaves the
			// section unset still gets the defaults (auto_lanes_max 2,
			// special priority 1) applied.
			return config.EffectiveUsageCalculator(cfg.Usage.Calculator)
		},
		logStore: logStore,
		now:      time.Now,
		logf:     log.Printf,
	}
	if svc != nil {
		w.advisor = svc
	}
	if reg != nil {
		w.target = func() (*inference.LLM, bool) {
			for _, l := range reg.List() {
				if l != nil && l.Kind == inference.KindClaudeCode && l.Enabled() {
					return l, true
				}
			}
			return nil, false
		}
	}
	w.StartHourly(ctx)
}

// errAutoLanesNoRegistry is the save-path error when the LLM registry is
// missing.
var errAutoLanesNoRegistry = errors.New("auto-lanes: no LLM registry")
