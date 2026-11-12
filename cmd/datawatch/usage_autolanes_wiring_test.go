// usage_autolanes_wiring_test.go — the wiring tests for the hourly auto-lanes
// loop: the run-once-at-start + each-tick rhythm, the rule as seen through the
// fake usage service, the operator maximum, the unchanged case, the off
// switch and the entry pick. The loop under test is the production code in
// usage_autolanes_wiring.go with the fake usage service, the fake clock and
// the fake special count in place; the LLM entries and their save path are the
// real inference.Registry, exactly the path the operator's LLM update uses.
package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// autoLanesTestAdvisor is the fake usage service: it answers every Advice call
// with one fixed result (or one fixed error) and counts the calls, so a test
// can assert how many times the loop actually asked for advice.
type autoLanesTestAdvisor struct {
	res   *usagecalc.AdviceResult
	err   error
	calls int
}

func (a *autoLanesTestAdvisor) Advice(ctx context.Context, opts usagecalc.Options) (*usagecalc.AdviceResult, error) {
	a.calls++
	return a.res, a.err
}

// autoLanesAdvice is a minimal AdviceResult carrying the suggested lanes, the
// resolved policy and a live "lanes running now" reading for the log entry.
func autoLanesAdvice(lanesSuggested int, policy string) *usagecalc.AdviceResult {
	runningNow := 2
	return &usagecalc.AdviceResult{
		Policy: policy,
		Advice: usagecalc.Advice{
			Advice:          usagecalc.PolicyAdvice{Policy: policy, LanesSuggested: lanesSuggested},
			LanesRunningNow: &runningNow,
		},
	}
}

// autoLanesConfigOn is the settings shape the on-switch tests start from:
// auto-lanes on, the given operator maximum, special priority 1 — the live
// settings the loop re-reads on every pass.
func autoLanesConfigOn(max int) config.UsageCalculatorConfig {
	return config.UsageCalculatorConfig{
		AutoLanes:                true,
		AutoLanesMax:             max,
		AutoLanesSpecialPriority: 1,
	}
}

// pickFirstClaudeCode is the production target function over a registry: the
// first enabled claude-code entry in list order.
func pickFirstClaudeCode(reg *inference.Registry) func() (*inference.LLM, bool) {
	return func() (*inference.LLM, bool) {
		for _, l := range reg.List() {
			if l != nil && l.Kind == inference.KindClaudeCode && l.Enabled() {
				return l, true
			}
		}
		return nil, false
	}
}

// newAutoLanesWiringTest builds the loop under test: fake advisor, real
// registry seeded with the given entries (target pick plus save path through
// Registry.Update — the operator's LLM update path), fake special count, live
// settings, a real decision log in a temp dir and a stable clock.
func newAutoLanesWiringTest(t *testing.T, advisor *autoLanesTestAdvisor, cfg config.UsageCalculatorConfig, entries []*inference.LLM, specialCount int) (*autoLanesWiring, *usagecalc.AutoLanesLog, *inference.Registry) {
	t.Helper()
	reg := inference.NewRegistry()
	for _, e := range entries {
		if err := reg.Add(e); err != nil {
			t.Fatalf("Add %s: %v", e.Name, err)
		}
	}
	logStore, err := usagecalc.OpenAutoLanesLog(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAutoLanesLog: %v", err)
	}
	fixed := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	return &autoLanesWiring{
		advisor:  advisor,
		target:   pickFirstClaudeCode(reg),
		apply:    reg.Update,
		special:  func(threshold int) int { return specialCount },
		usage:    func() config.UsageCalculatorConfig { return cfg },
		logStore: logStore,
		now:      func() time.Time { return fixed },
	}, logStore, reg
}

// autoLanesGetMax reads one entry's current max_inflight out of the registry.
func autoLanesGetMax(t *testing.T, reg *inference.Registry, name string) int {
	t.Helper()
	l, err := reg.Get(name)
	if err != nil {
		t.Fatalf("Get %s: %v", name, err)
	}
	return l.MaxInflight
}

// awaitAutoLanes polls until cond holds or the deadline passes; it is the
// test synchronization point against the loop's goroutine.
func awaitAutoLanes(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestAutoLanesRunsOnceAtStartAndEachHour pins the loop's rhythm: one pass
// before the first tick, one pass per tick — each pass exactly one advice
// call, so a hung or missing tick is visible as a missing pass.
func TestAutoLanesRunsOnceAtStartAndEachHour(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(1, "use-all")}
	w, _, _ := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(4),
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 1}}, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tick := make(chan time.Time, 4)
	go w.Start(ctx, tick)

	awaitAutoLanes(t, func() bool { return advisor.calls == 1 }, "start pass")
	tick <- time.Now()
	awaitAutoLanes(t, func() bool { return advisor.calls == 2 }, "first tick pass")
	tick <- time.Now()
	awaitAutoLanes(t, func() bool { return advisor.calls == 3 }, "second tick pass")
}

// TestAutoLanesAppliesAdviceWithinOperatorMaximum pins the happy move: advice
// of 3 lanes plus 2 special items is 5, the operator maximum of 4 clamps it
// to 4, the cap moves 0 → 4 through the registry, and the decision — with
// the policy, lanes running now and the special count — lands in the log.
func TestAutoLanesAppliesAdviceWithinOperatorMaximum(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(3, "use-all")}
	w, logStore, reg := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(4),
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 0}}, 2)

	w.runOnce(context.Background())

	if got := autoLanesGetMax(t, reg, "cc"); got != 4 {
		t.Fatalf("cc max_inflight = %d, want 4 (advice 3 + special 2 clamped to maximum 4)", got)
	}
	e := logStore.Entries()
	if len(e) != 1 {
		t.Fatalf("log entries = %d, want 1", len(e))
	}
	if e[0].From != 0 || e[0].To != 4 || e[0].Direction != usagecalc.AutoLanesUp {
		t.Fatalf("entry = %+v, want 0 -> 4 up", e[0])
	}
	if e[0].Policy != "use-all" || e[0].SpecialItems != 2 || e[0].LanesRunning != 2 {
		t.Fatalf("entry context = %+v, want policy use-all, 2 special, 2 running", e[0])
	}
}

// TestAutoLanesNeverExceedsOperatorMaximum pins the other side of the clamp:
// even with 6 special items open, the applied cap stays at the operator
// maximum of 2 — the rule clamps the sum, so auto-lanes can never run the
// daemon past the ceiling the operator set.
func TestAutoLanesNeverExceedsOperatorMaximum(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(3, "pace")}
	w, _, reg := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(2),
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 1}}, 6)

	w.runOnce(context.Background())

	if got := autoLanesGetMax(t, reg, "cc"); got != 2 {
		t.Fatalf("cc max_inflight = %d, want 2 (operator maximum)", got)
	}
	if got := autoLanesGetMax(t, reg, "cc"); got > 2 {
		t.Fatalf("cc max_inflight %d exceeds operator maximum 2", got)
	}
}

// TestAutoLanesKeepsAtLeastOneLane pins the lower bound: a reading of zero
// lanes with no special items is 0 + 0, clamped to 1 — the cap falls to
// exactly one lane, never zero, never below.
func TestAutoLanesKeepsAtLeastOneLane(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(0, "use-all")}
	w, logStore, reg := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(3),
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 3}}, 0)

	w.runOnce(context.Background())

	if got := autoLanesGetMax(t, reg, "cc"); got != 1 {
		t.Fatalf("cc max_inflight = %d, want 1 (the floor)", got)
	}
	e := logStore.Entries()
	if len(e) != 1 || e[0].To != 1 || e[0].Direction != usagecalc.AutoLanesDown {
		t.Fatalf("log = %+v, want one 3 -> 1 down entry", e)
	}
}

// TestAutoLanesLeavesCapWhenTargetMatches pins the unchanged case: advice 1
// plus 0 special is already the configured cap of 1, so the registry is not
// touched — no spurious churn of the LLM entry — but the decision is still
// logged, because it is the record of why the daemon weighed this reading.
func TestAutoLanesLeavesCapWhenTargetMatches(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(1, "use-all")}
	w, logStore, reg := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(8),
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 1}}, 0)

	w.runOnce(context.Background())

	if got := autoLanesGetMax(t, reg, "cc"); got != 1 {
		t.Fatalf("cc max_inflight = %d, want 1 (unchanged)", got)
	}
	e := logStore.Entries()
	if len(e) != 1 || e[0].Direction != usagecalc.AutoLanesUnchanged || e[0].From != 1 || e[0].To != 1 {
		t.Fatalf("log = %+v, want one 1 -> 1 unchanged entry", e)
	}
	if e[0].At.IsZero() {
		t.Fatalf("log entry has no timestamp: %+v", e[0])
	}
}

// TestAutoLanesOffDoesNothing pins the default: with
// usage.calculator.auto_lanes false the loop neither asks for advice nor moves
// the cap — the entry is left exactly as configured and no log entry is
// written, so lane changes stay with the operator.
func TestAutoLanesOffDoesNothing(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(3, "use-all")}
	w, logStore, reg := newAutoLanesWiringTest(t, advisor, config.UsageCalculatorConfig{},
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 0}}, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tick := make(chan time.Time, 2)
	go w.Start(ctx, tick)
	tick <- time.Now()

	// The off switch is a silent skip; give the (not-taken) path a moment to
	// be visibly absent, and a real failure a moment to be visible.
	time.Sleep(50 * time.Millisecond)
	if advisor.calls != 0 {
		t.Fatalf("advisor called %d time(s), want 0 while auto_lanes is false", advisor.calls)
	}
	if got := autoLanesGetMax(t, reg, "cc"); got != 0 {
		t.Fatalf("cc max_inflight = %d, want 0 (never touched while auto_lanes is false)", got)
	}
	if got := logStore.Len(); got != 0 {
		t.Fatalf("log has %d entries, want 0 while auto_lanes is false", got)
	}
}

// TestAutoLanesAppliesOnlyTheTargetEntry pins the entry pick: the first
// enabled claude-code entry in list order is the one capped; the disabled
// claude-code entry and the enabled claude (API) entry are left exactly as
// configured, and the save goes out through the real Registry.Update.
func TestAutoLanesAppliesOnlyTheTargetEntry(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(2, "use-all")}
	w, logStore, reg := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(4), []*inference.LLM{
		{Name: "aa-disabled", Kind: inference.KindClaudeCode, MaxInflight: 5, Disabled: true},
		{Name: "bb-code", Kind: inference.KindClaudeCode, MaxInflight: 1},
		{Name: "cc-api", Kind: inference.KindClaude, MaxInflight: 7},
	}, 1)

	w.runOnce(context.Background())

	if got := autoLanesGetMax(t, reg, "aa-disabled"); got != 5 {
		t.Fatalf("aa-disabled max_inflight = %d, want 5 (disabled entries are never capped)", got)
	}
	if got := autoLanesGetMax(t, reg, "bb-code"); got != 3 {
		t.Fatalf("bb-code max_inflight = %d, want 3 (advice 2 + special 1)", got)
	}
	if got := autoLanesGetMax(t, reg, "cc-api"); got != 7 {
		t.Fatalf("cc-api max_inflight = %d, want 7 (non claude-code entries are never capped)", got)
	}
	e := logStore.Entries()
	if len(e) != 1 || e[0].To != 3 {
		t.Fatalf("log = %+v, want one entry toward 3", e)
	}
}

// TestAutoLanesAdviceErrorSkipsThePass pins the failure path: an advice
// failure is a skipped pass — no cap change, no log entry — and the next
// pass applies normally when the advisor recovers.
func TestAutoLanesAdviceErrorSkipsThePass(t *testing.T) {
	advisor := &autoLanesTestAdvisor{err: errors.New("backend down")}
	w, logStore, reg := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(4),
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 2}}, 1)

	w.runOnce(context.Background())

	if got := autoLanesGetMax(t, reg, "cc"); got != 2 {
		t.Fatalf("cc max_inflight = %d, want 2 (unchanged on advice error)", got)
	}
	if got := logStore.Len(); got != 0 {
		t.Fatalf("log has %d entries, want 0 on advice error", got)
	}

	advisor.err = nil
	advisor.res = autoLanesAdvice(3, "use-all")
	w.runOnce(context.Background())
	if got := autoLanesGetMax(t, reg, "cc"); got != 4 {
		t.Fatalf("cc max_inflight = %d, want 4 on the recovered pass", got)
	}
}

// TestAutoLanesSaveErrorStillLogsTheDecision pins the order of effects: the
// decision is appended to the log before it is applied, so a failed save
// (registry validation, disk) still leaves the record the operator can see —
// with the intended target — while the cap in the registry stays as it was.
func TestAutoLanesSaveErrorStillLogsTheDecision(t *testing.T) {
	advisor := &autoLanesTestAdvisor{res: autoLanesAdvice(3, "use-all")}
	w, logStore, reg := newAutoLanesWiringTest(t, advisor, autoLanesConfigOn(4),
		[]*inference.LLM{{Name: "cc", Kind: inference.KindClaudeCode, MaxInflight: 1}}, 0)
	w.apply = func(l *inference.LLM) error { return errors.New("llm: refused in tests") }

	w.runOnce(context.Background())

	if got := autoLanesGetMax(t, reg, "cc"); got != 1 {
		t.Fatalf("cc max_inflight = %d, want 1 (failed save must not move the cap)", got)
	}
	e := logStore.Entries()
	if len(e) != 1 || e[0].From != 1 || e[0].To != 3 || e[0].Direction != usagecalc.AutoLanesUp {
		t.Fatalf("log = %+v, want one 1 -> 3 up entry even when the save failed", e)
	}
}
