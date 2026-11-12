// usage_autolanes_smoke_test.go — the in-process smoke check of the auto-lanes
// feature (usage.calculator.auto_lanes), loop side. It feeds the hourly pass
// recorded usage readings from a fake source under a fixed clock, through the
// real usage calculator service and the real decision log, and confirms the
// lane count chosen, the change-log entry written, the apply step through a
// fake settings writer, and that the operator's own cap is never touched while
// the feature is off. No daemon, no config or data folder of the operator, and
// no model are involved: every directory is a temporary one.
package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// smokeNow is the fixed clock every smoke check runs under (a Wednesday).
var smokeNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// Recorded reading series, as percent of the weekly allowance at 48h, 24h and
// 1h before smokeNow. The burn figure is the difference over time, so a flat
// series is a light burn and a steep one is a heavy burn.
var (
	smokeLightBurn  = []float64{10, 11, 12} // advice suggests 3 lanes
	smokeMediumBurn = []float64{10, 25, 40} // advice suggests 2 lanes
	smokeHeavyBurn  = []float64{20, 50, 80} // advice suggests 1 lane
)

// smokeSource is the fake readings source: it records percent readings into a
// temporary usage store on the fixed clock, the same store the real calculator
// service reads.
type smokeSource struct {
	t     *testing.T
	store *usagecalc.Store
}

func newSmokeSource(t *testing.T) *smokeSource {
	t.Helper()
	store, err := usagecalc.OpenStore(t.TempDir(), func() time.Time { return smokeNow })
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return &smokeSource{t: t, store: store}
}

// record replaces the recorded series with the given percents at 48h, 24h, 1h
// before the fixed clock.
func (s *smokeSource) record(percents []float64) {
	s.t.Helper()
	for _, r := range s.store.List(time.Time{}) {
		if err := s.store.Delete(r.ID); err != nil {
			s.t.Fatalf("Delete: %v", err)
		}
	}
	for i, hoursAgo := range []int{48, 24, 1} {
		p := percents[i]
		_, err := s.store.Append(usagecalc.Reading{
			At:      smokeNow.Add(-time.Duration(hoursAgo) * time.Hour),
			Percent: &p,
			Source:  usagecalc.SourceTyped,
		})
		if err != nil {
			s.t.Fatalf("Append: %v", err)
		}
	}
}

// service builds the real calculator service over the fake source with the
// fixed clock, reading its settings live from the given config pointer.
func (s *smokeSource) service(cfg *config.UsageCalculatorConfig) *usagecalc.Service {
	clock := func() time.Time { return smokeNow }
	series := usagecalc.NewSharedSeries(s.store, "", nil, usagecalc.WithSharedClock(clock))
	return usagecalc.NewService(series, s.store,
		func() usagecalc.Settings { return server.UsageCalculatorSettings(config.UsageConfig{Calculator: *cfg}) },
		nil, usagecalc.WithServiceNow(clock))
}

// smokeWriter is the fake settings writer: it records every apply call and can
// be told to fail.
type smokeWriter struct {
	calls []int
	err   error
}

func (w *smokeWriter) apply(l *inference.LLM) error {
	w.calls = append(w.calls, l.MaxInflight)
	return w.err
}

// smokeRig is one pass under test with every collaborator fake or temporary.
type smokeRig struct {
	src     *smokeSource
	cfg     *config.UsageCalculatorConfig
	entry   *inference.LLM
	writer  *smokeWriter
	special *int
	logs    *usagecalc.AutoLanesLog
	wiring  *autoLanesWiring
	lines   []string
}

func newSmokeRig(t *testing.T, enabled bool, maximum, currentCap, special int, percents []float64) *smokeRig {
	t.Helper()
	r := &smokeRig{
		src:     newSmokeSource(t),
		cfg:     &config.UsageCalculatorConfig{AutoLanes: enabled, AutoLanesMax: maximum, AutoLanesSpecialPriority: 1},
		entry:   &inference.LLM{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: currentCap},
		writer:  &smokeWriter{},
		special: &special,
	}
	r.src.record(percents)
	var err error
	if r.logs, err = usagecalc.OpenAutoLanesLog(t.TempDir()); err != nil {
		t.Fatalf("OpenAutoLanesLog: %v", err)
	}
	r.wiring = &autoLanesWiring{
		advisor:  r.src.service(r.cfg),
		target:   func() (*inference.LLM, bool) { return r.entry, true },
		apply:    r.writer.apply,
		special:  func(int) int { return *r.special },
		usage:    func() config.UsageCalculatorConfig { return config.EffectiveUsageCalculator(*r.cfg) },
		logStore: r.logs,
		now:      func() time.Time { return smokeNow },
		logf:     func(f string, a ...any) { r.lines = append(r.lines, f) },
	}
	return r
}

func (r *smokeRig) pass() { r.wiring.runOnce(context.Background()) }

// TestAutoLanesSmokeChoosesLaneCountFromReadings: light, medium and heavy
// recorded burn give 3, 2 and 1 lane(s) when the cap starts elsewhere; the
// fake writer receives exactly that count.
func TestAutoLanesSmokeChoosesLaneCountFromReadings(t *testing.T) {
	cases := []struct {
		name     string
		percents []float64
		current  int
		want     int
		dir      string
	}{
		{"light burn raises the cap", smokeLightBurn, 1, 3, usagecalc.AutoLanesUp},
		{"heavy burn lowers the cap", smokeHeavyBurn, 3, 1, usagecalc.AutoLanesDown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newSmokeRig(t, true, 4, c.current, 0, c.percents)
			r.pass()
			if len(r.writer.calls) != 1 || r.writer.calls[0] != c.want {
				t.Fatalf("writer calls = %v, want exactly [%d]", r.writer.calls, c.want)
			}
			entries := r.logs.Entries()
			if len(entries) != 1 {
				t.Fatalf("log has %d entries, want 1", len(entries))
			}
			e := entries[0]
			if e.From != c.current || e.To != c.want || e.Direction != c.dir {
				t.Errorf("log entry = %d -> %d (%s), want %d -> %d (%s)", e.From, e.To, e.Direction, c.current, c.want, c.dir)
			}
			if !e.At.Equal(smokeNow) {
				t.Errorf("log entry time = %v, want the fixed clock %v", e.At, smokeNow)
			}
			if e.Policy != usagecalc.PolicyUseAll {
				t.Errorf("log entry policy = %q, want %q", e.Policy, usagecalc.PolicyUseAll)
			}
			if !strings.Contains(e.Reason, "advice reading") {
				t.Errorf("log entry reason = %q, want it to name the advice reading", e.Reason)
			}
		})
	}
}

// TestAutoLanesSmokeUnchangedIsLoggedNotApplied: when the target equals the
// cap in force, a decision is recorded but the writer is never called.
func TestAutoLanesSmokeUnchangedIsLoggedNotApplied(t *testing.T) {
	r := newSmokeRig(t, true, 4, 2, 0, smokeMediumBurn)
	r.pass()
	if len(r.writer.calls) != 0 {
		t.Fatalf("writer calls = %v, want none for an unchanged cap", r.writer.calls)
	}
	entries := r.logs.Entries()
	if len(entries) != 1 || entries[0].Direction != usagecalc.AutoLanesUnchanged || entries[0].From != 2 || entries[0].To != 2 {
		t.Fatalf("log = %+v, want one unchanged 2 -> 2 entry", entries)
	}
}

// TestAutoLanesSmokeNeverExceedsOperatorMaximum: light burn suggests 3 lanes
// and 2 special items would make 5; the operator maximum of 4 wins, and a
// maximum of 2 wins over the plain advice of 3.
func TestAutoLanesSmokeNeverExceedsOperatorMaximum(t *testing.T) {
	r := newSmokeRig(t, true, 4, 1, 2, smokeLightBurn)
	r.pass()
	if len(r.writer.calls) != 1 || r.writer.calls[0] != 4 {
		t.Fatalf("with special items, writer calls = %v, want [4] (the operator maximum)", r.writer.calls)
	}
	if got := r.logs.Entries()[0].SpecialItems; got != 2 {
		t.Errorf("log special_items = %d, want 2", got)
	}

	r2 := newSmokeRig(t, true, 2, 1, 0, smokeLightBurn)
	r2.pass()
	if len(r2.writer.calls) != 1 || r2.writer.calls[0] != 2 {
		t.Fatalf("with maximum 2, writer calls = %v, want [2]", r2.writer.calls)
	}
}

// TestAutoLanesSmokeOffNeverOverridesOperator: with the feature off, a pass
// asks nothing of the advice service, writes nothing, logs nothing and leaves
// the operator's own cap exactly as set — even when the readings would move it.
func TestAutoLanesSmokeOffNeverOverridesOperator(t *testing.T) {
	for _, percents := range [][]float64{smokeLightBurn, smokeMediumBurn, smokeHeavyBurn} {
		r := newSmokeRig(t, false, 4, 3, 2, percents)
		counting := &autoLanesTestAdvisor{res: autoLanesAdvice(1, "use-all")}
		r.wiring.advisor = counting
		for i := 0; i < 3; i++ {
			r.pass()
		}
		if len(r.writer.calls) != 0 {
			t.Errorf("off: writer calls = %v, want none", r.writer.calls)
		}
		if r.entry.MaxInflight != 3 {
			t.Errorf("off: operator cap = %d, want 3 untouched", r.entry.MaxInflight)
		}
		if r.logs.Len() != 0 {
			t.Errorf("off: log has %d entries, want none", r.logs.Len())
		}
		if counting.calls != 0 {
			t.Errorf("off: advice asked %d times, want 0", counting.calls)
		}
	}
}

// TestAutoLanesSmokeOffThroughRealRegistry: the same off guarantee on the real
// LLM registry — the operator's saved max_inflight reads back unchanged after
// passes with the feature off, and changes only once it is switched on.
func TestAutoLanesSmokeOffThroughRealRegistry(t *testing.T) {
	r := newSmokeRig(t, false, 4, 1, 0, smokeLightBurn)
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 3}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	r.wiring.target = pickFirstClaudeCode(reg)
	r.wiring.apply = reg.Update

	r.pass()
	r.pass()
	if got := autoLanesGetMax(t, reg, "cc"); got != 3 {
		t.Fatalf("off: registry max_inflight = %d, want the operator's 3", got)
	}
	r.cfg.AutoLanes = true // the operator flips the switch; the next pass sees it
	r.pass()
	if got := autoLanesGetMax(t, reg, "cc"); got != 3 {
		// light burn suggests 3 lanes, which already equals the operator's cap
		t.Fatalf("on, light burn: registry max_inflight = %d, want 3", got)
	}
	r.src.record(smokeHeavyBurn)
	r.pass()
	if got := autoLanesGetMax(t, reg, "cc"); got != 1 {
		t.Fatalf("on, heavy burn: registry max_inflight = %d, want 1", got)
	}
	r.cfg.AutoLanes = false
	r.src.record(smokeLightBurn)
	r.pass()
	if got := autoLanesGetMax(t, reg, "cc"); got != 1 {
		t.Fatalf("off again: registry max_inflight = %d, want it left at 1", got)
	}
}

// TestAutoLanesSmokeHourlyTicksFollowReadings: the loop runs once at start and
// once per fake hourly tick, and each pass uses the readings recorded at that
// moment: light, then heavy, then medium burn walk the cap 1 -> 3 -> 1 -> 2.
func TestAutoLanesSmokeHourlyTicksFollowReadings(t *testing.T) {
	r := newSmokeRig(t, true, 4, 1, 0, smokeLightBurn)
	// The fake writer also updates the entry, as the real registry save does.
	r.wiring.apply = func(l *inference.LLM) error {
		r.writer.calls = append(r.writer.calls, l.MaxInflight)
		r.entry.MaxInflight = l.MaxInflight
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() { r.wiring.Start(ctx, tick); close(done) }()

	awaitAutoLanes(t, func() bool { return r.logs.Len() == 1 }, "start pass")
	r.src.record(smokeHeavyBurn)
	tick <- smokeNow.Add(time.Hour)
	awaitAutoLanes(t, func() bool { return r.logs.Len() == 2 }, "first tick pass")
	r.src.record(smokeMediumBurn)
	tick <- smokeNow.Add(2 * time.Hour)
	awaitAutoLanes(t, func() bool { return r.logs.Len() == 3 }, "second tick pass")
	cancel()
	<-done

	want := []int{3, 1, 2}
	if len(r.writer.calls) != len(want) {
		t.Fatalf("writer calls = %v, want %v", r.writer.calls, want)
	}
	for i, w := range want {
		if r.writer.calls[i] != w {
			t.Errorf("writer call %d = %d, want %d", i, r.writer.calls[i], w)
		}
	}
}

// TestAutoLanesSmokeApplyFailureKeepsRecord: a failing settings writer leaves
// the cap unchanged but the decision still lands in the log, so the failure
// stays visible.
func TestAutoLanesSmokeApplyFailureKeepsRecord(t *testing.T) {
	r := newSmokeRig(t, true, 4, 1, 0, smokeLightBurn)
	r.writer.err = errors.New("save refused")
	r.pass()
	if len(r.writer.calls) != 1 {
		t.Fatalf("writer calls = %v, want one attempt", r.writer.calls)
	}
	if r.entry.MaxInflight != 1 {
		t.Errorf("cap = %d, want 1 unchanged after a failed save", r.entry.MaxInflight)
	}
	if r.logs.Len() != 1 {
		t.Errorf("log has %d entries, want the decision recorded", r.logs.Len())
	}
}

// TestAutoLanesSmokeNoReadingsSettlesAtOneLane: with no recorded readings the
// advice names zero lanes; the rule never applies zero, so the cap settles at
// 1 lane and the decision is logged.
func TestAutoLanesSmokeNoReadingsSettlesAtOneLane(t *testing.T) {
	r := newSmokeRig(t, true, 4, 2, 0, smokeLightBurn)
	for _, rd := range r.src.store.List(time.Time{}) {
		if err := r.src.store.Delete(rd.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	r.pass()
	if len(r.writer.calls) != 1 || r.writer.calls[0] != 1 {
		t.Fatalf("writer calls = %v, want exactly [1] (never zero)", r.writer.calls)
	}
	entries := r.logs.Entries()
	if len(entries) != 1 || entries[0].From != 2 || entries[0].To != 1 {
		t.Fatalf("log = %+v, want one 2 -> 1 entry", entries)
	}
}
