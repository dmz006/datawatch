package usagecalc

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Service test data: a Sunday 08:00 UTC weekly reset, the shared reading at Thursday
// 08:00 UTC, and the service clock at Sunday 12:00 UTC — eighty hours after the reading.
var (
	serviceReset      = ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "UTC"}
	serviceReadingAt  = time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	serviceReadingPct = 40.0
	serviceReadingLn  = 3
	serviceNow        = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
)

// fakeQueue is a QueueCounter the tests can resize.
type fakeQueue struct{ n int }

func (q fakeQueue) QueuedTaskCount() int { return q.n }

// serviceFixture builds a keeper-mode service over a fresh store.
type serviceFixture struct {
	t      *testing.T
	clock  *fakeClock
	store  *Store
	series *SharedSeries
	svc    *Service
}

func newServiceFixture(t *testing.T, settings Settings, queue QueueCounter) *serviceFixture {
	t.Helper()
	clock := newFakeClock(serviceNow)
	store, err := OpenStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	series := NewSharedSeries(store, "", nil, WithSharedClock(clock.Now))
	svc := NewService(series, store, func() Settings { return settings }, queue, WithServiceNow(clock.Now))
	return &serviceFixture{t: t, clock: clock, store: store, series: series, svc: svc}
}

// seedReading appends the shared reading of the fixture.
func (f *serviceFixture) seedReading(t *testing.T) {
	t.Helper()
	pct, lanes := serviceReadingPct, serviceReadingLn
	if _, err := f.store.Append(Reading{At: serviceReadingAt, Percent: &pct, Lanes: &lanes, LanesSource: LanesLive, Source: SourceTyped}); err != nil {
		t.Fatalf("append reading: %v", err)
	}
}

func TestServiceAdviceBasics(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t)

	res, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Stale {
		t.Errorf("Stale: got true on the keeper; want false")
	}
	if res.Variant != VariantAll || res.Policy != PolicyUseAll {
		t.Errorf("defaults: variant=%q policy=%q; want all/use-all", res.Variant, res.Policy)
	}
	if res.Advice.Needs != "" {
		t.Errorf("Needs: got %q; want empty", res.Advice.Needs)
	}
	if got := res.Advice.Percent; math.Abs(got-serviceReadingPct) > 1e-9 {
		t.Errorf("Percent: got %v; want %v", got, serviceReadingPct)
	}
	if res.Advice.LatestAgeHours == nil || math.Abs(*res.Advice.LatestAgeHours-76) > 1e-9 {
		t.Errorf("LatestAgeHours: got %v; want 76", res.Advice.LatestAgeHours)
	}
	if res.Advice.Lanes != 3 {
		t.Errorf("Lanes: got %d; want the reading's 3", res.Advice.Lanes)
	}
	if math.Abs(round1(res.Advice.PaceLine)-57.1) > 0.051 {
		t.Errorf("PaceLine: got %v; want about 57.1", res.Advice.PaceLine)
	}
	if res.Advice.Verdict == "" {
		t.Errorf("Verdict is empty; the reading carries a percentage")
	}
	if len(res.Advice.Series) != 1 {
		t.Errorf("Series: got %d point; want 1", len(res.Advice.Series))
	}
	if res.Advice.ExtraCreditPoints != 0 || res.Advice.Ceiling != WeeklyAllowance {
		t.Errorf("extra credit unset: ceiling %v, extra %v; want 100 and 0", res.Advice.Ceiling, res.Advice.ExtraCreditPoints)
	}

	// Extra credit is one account fact in the shared record: the ceiling follows it.
	if err := f.store.SetExtraCredit(25); err != nil {
		t.Fatalf("SetExtraCredit: %v", err)
	}
	res, err = f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice after extra credit: %v", err)
	}
	if res.Advice.Ceiling != 125 {
		t.Errorf("Ceiling after 25 extra points: got %v; want 125", res.Advice.Ceiling)
	}

	// A series with no usable reading waits for one, and says so.
	empty := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	res, err = empty.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice on an empty series: %v", err)
	}
	if res.Advice.Needs != NeedsReading {
		t.Errorf("Needs: got %q; want %q", res.Advice.Needs, NeedsReading)
	}
}

func TestServiceVariantNeverChangesNumbers(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset, Variant: VariantLanePlanner}, nil)
	f.seedReading(t)
	ctx := context.Background()

	res, err := f.svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Variant != VariantLanePlanner {
		t.Errorf("stored variant: got %q; want %q", res.Variant, VariantLanePlanner)
	}

	// The variant picks the rendering, nothing else: all four variant values serve the
	// same reading, the same lane table and the same day totals, so every variant must
	// carry the same numbers.
	variants := []string{VariantPaceGauge, VariantLanePlanner, VariantBurnLedger, VariantAll}
	for _, v := range variants {
		got, err := f.svc.Advice(ctx, Options{Variant: v})
		if err != nil {
			t.Fatalf("Advice with %s: %v", v, err)
		}
		if got.Variant != v {
			t.Errorf("variant %s: the response names %q instead", v, got.Variant)
		}
		if !reflect.DeepEqual(got.Advice, res.Advice) {
			t.Errorf("variant %s changed the numbers:\n got %+v\nwant %+v", v, got.Advice, res.Advice)
		}
	}

	// A variant name the operator does not know is a selection error, and the response the
	// request the operator can fix on the next call.
	_, err = f.svc.Advice(ctx, Options{Variant: "no-such-form"})
	if err == nil || !strings.Contains(err.Error(), "variant") {
		t.Errorf("unknown variant: got err=%v; want an error naming the variant", err)
	}

	// An unset stored variant keeps the model's numbers and falls back to the full form.
	all := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	all.seedReading(t)
	res, err = all.svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("Advice with no stored variant: %v", err)
	}
	if res.Variant != VariantAll {
		t.Errorf("default variant: got %q; want %q", res.Variant, VariantAll)
	}
}

func TestServiceRetiredVariantFallsBack(t *testing.T) {
	// A stored setting that names a variant removed from the allowed list falls back to
	// the full form: the service serves the advice without an error, names all in the
	// response, and keeps the model's numbers.
	f := newServiceFixture(t, Settings{Reset: serviceReset, Variant: "old-pulse"}, nil)
	f.seedReading(t)
	ctx := context.Background()

	res, err := f.svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("Advice with a retired stored variant: %v", err)
	}
	if res.Variant != VariantAll {
		t.Errorf("retired stored variant: the response names %q; want the fallback %q", res.Variant, VariantAll)
	}

	// The fallback keeps the model's numbers: identical to the same settings with the
	// retired value cleared.
	clear := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	clear.seedReading(t)
	cleared, err := clear.svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("Advice with the setting cleared: %v", err)
	}
	if !reflect.DeepEqual(res.Advice, cleared.Advice) {
		t.Errorf("the fallback kept different numbers:\n got %+v\nwant %+v", res.Advice, cleared.Advice)
	}

	// A per-call request for a retired name still fails: it is a selection the operator
	// can fix, not a saved setting that got retired under them.
	if _, err := f.svc.Advice(ctx, Options{Variant: "old-pulse"}); err == nil {
		t.Errorf("retired per-call variant: want a selection error")
	}
}

// serviceNumbers is the advice with the advice object cleared, the set of numbers the
// reading alone produced. Variants and policies must never change it.
func serviceNumbers(res *AdviceResult) Advice {
	a := res.Advice
	a.Advice = PolicyAdvice{}
	return a
}

func TestServicePolicyOverrideChangesOnlyAdvice(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t)
	ctx := context.Background()
	if err := f.svc.SetRemainingWork(ctx, ptr64(15)); err != nil {
		t.Fatalf("SetRemainingWork: %v", err)
	}

	// The stored policy is empty: use-all plans the whole allowance.
	res, err := f.svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Policy != PolicyUseAll {
		t.Errorf("stored policy: got %q; want %q", res.Policy, PolicyUseAll)
	}

	// The policy picks the target and the words around it: every policy keeps the
	// numbers the reading alone produced.
	policies := []string{PolicyUseAll, PolicyPace, PolicyFinishEarly}
	for _, p := range policies {
		got, err := f.svc.Advice(ctx, Options{Policy: p})
		if err != nil {
			t.Fatalf("Advice with %s policy: %v", p, err)
		}
		if got.Policy != p {
			t.Errorf("policy %s: the response names %q instead", p, got.Policy)
		}
		if !reflect.DeepEqual(serviceNumbers(got), serviceNumbers(res)) {
			t.Errorf("policy %s changed the numbers of the reading:\n got %+v\nwant %+v", p, got.Advice, res.Advice)
		}
	}

	// A per-call policy is independent of the variant and the stored policy.
	res, err = f.svc.Advice(ctx, Options{Variant: VariantPaceGauge, Policy: PolicyPace})
	if err != nil {
		t.Fatalf("Advice with pace: %v", err)
	}
	if res.Policy != PolicyPace {
		t.Errorf("override policy: got %q; want %q", res.Policy, PolicyPace)
	}
	if res.Advice.Advice.LanesSuggested < 1 {
		t.Errorf("pace policy must name at least one lane; lanes suggested is 0")
	}

	// Run flat out finishes the typed remaining work before the allowance.
	res, err = f.svc.Advice(ctx, Options{Policy: PolicyFinishEarly})
	if err != nil {
		t.Fatalf("Advice with finish-early: %v", err)
	}
	if res.Policy != PolicyFinishEarly {
		t.Errorf("finish-early policy: got %q", res.Policy)
	}
	if res.Advice.Advice.Finish == nil {
		t.Errorf("finish-early has a typed figure of 15 points but no finish line")
	} else {
		if res.Advice.Advice.Finish.NoBurn {
			t.Errorf("finish line: NoBurn on a reading that burns above the baseline")
		}
		if res.Advice.Advice.Finish.Hours == nil || *res.Advice.Advice.Finish.Hours <= 0 {
			t.Errorf("finish hours: got %v; want a positive number", res.Advice.Advice.Finish.Hours)
		}
		if res.Advice.Advice.Finish.EndPercent == nil {
			t.Errorf("finish end percent: got nil; want a number")
		}
	}

	// A per-call policy that no variant recognizes is the caller's mistake: it is
	// rejected, the REST layer answers 400 (smoke 82). The stored policy is a
	// setting, so a retired value there still normalizes to the default.
	res, err = f.svc.Advice(ctx, Options{Policy: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown usage calculator policy") {
		t.Fatalf("unknown policy: err=%v res=%v; want an unknown-policy selection error", err, res)
	}
	res, err = f.svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("no policy override: %v", err)
	}
	if res.Policy != PolicyUseAll {
		t.Errorf("no policy override: got %q; want the default %q", res.Policy, PolicyUseAll)
	}
}

func TestServiceQueueEstimateAndTypedOverride(t *testing.T) {
	newEstimate := func(n int, cost float64) *serviceFixture {
		return newServiceFixture(t, Settings{Reset: serviceReset, CostPerTask: cost}, fakeQueue{n: n})
	}

	// Five queued tasks at 0.4 points each is a 2-point estimate (decision 6).
	res, err := newEstimate(5, 0.4).svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	w := res.Advice.RemainingWork
	if w.InUse != WorkInUseEstimate {
		t.Errorf("InUse: got %q; want %q", w.InUse, WorkInUseEstimate)
	}
	if w.QueueTasks != 5 || w.CostPerTask != 0.4 {
		t.Errorf("queue shown: tasks %d cost %v; want 5 and 0.4", w.QueueTasks, w.CostPerTask)
	}
	if w.EstimatePoints == nil || math.Abs(*w.EstimatePoints-2) > 1e-9 {
		t.Errorf("EstimatePoints: got %v; want 2", w.EstimatePoints)
	}
	if w.Points != 2 {
		t.Errorf("Points: got %v; want 2", w.Points)
	}

	// A per-call cost override prices the same queue at 5 points.
	cost1 := 1.0
	res, err = newEstimate(5, 0.4).svc.Advice(context.Background(), Options{CostPerTask: &cost1})
	if err != nil {
		t.Fatalf("Advice with cost override: %v", err)
	}
	if w := res.Advice.RemainingWork; w.Points != 5 {
		t.Errorf("override cost: Points got %v; want 5", w.Points)
	}

	// The typed figure replaces the estimate and is labelled typed, beside the queue.
	f := newEstimate(5, 0.4)
	if err := f.svc.SetRemainingWork(context.Background(), ptr64(12.5)); err != nil {
		t.Fatalf("SetRemainingWork: %v", err)
	}
	res, err = f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice after typed work: %v", err)
	}
	w = res.Advice.RemainingWork
	if w.InUse != WorkInUseTyped {
		t.Errorf("InUse: got %q; want %q", w.InUse, WorkInUseTyped)
	}
	if w.Points != 12.5 {
		t.Errorf("Points: got %v; want the typed 12.5", w.Points)
	}
	if w.EstimatePoints == nil || math.Abs(*w.EstimatePoints-2) > 1e-9 {
		t.Errorf("EstimatePoints must stay shown: got %v; want 2", w.EstimatePoints)
	}
	if w.TypedPoints == nil {
		t.Errorf("TypedPoints is nil; the typed figure must be shown")
	}

	// No queue and no cost: nothing is in use, and a wrong count is clamped.
	res, err = newEstimate(0, 0).svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice without work: %v", err)
	}
	if w := res.Advice.RemainingWork; w.InUse != WorkInUseNone || w.Points != 0 {
		t.Errorf("no work: InUse %q Points %v; want none/0", w.InUse, w.Points)
	}
	res, err = newEstimate(-3, 0.4).svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice with a negative queue: %v", err)
	}
	if w := res.Advice.RemainingWork; w.QueueTasks != 0 {
		t.Errorf("negative queue clamped: got %d; want 0", w.QueueTasks)
	}
}

// laneReading appends a percent reading with a lane stamp to the fixture's store.
func (f *serviceFixture) laneReading(at time.Time, pct float64, lanes *int, source string) {
	f.t.Helper()
	if _, err := f.store.Append(Reading{At: at, Percent: &pct, Lanes: lanes, LanesSource: source, Source: SourceTyped}); err != nil {
		f.t.Fatalf("append reading: %v", err)
	}
}

func (f *serviceFixture) advice(o Options) *AdviceResult {
	f.t.Helper()
	res, err := f.svc.Advice(context.Background(), o)
	if err != nil {
		f.t.Fatalf("Advice: %v", err)
	}
	return res
}

func TestServiceLanesChain(t *testing.T) {
	// The what-if, then the lanes of the newest stamped reading, then the stored maximum,
	// then the package default.
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t) // 3 lanes, live

	if res := f.advice(Options{}); res.Advice.Lanes != 3 {
		t.Errorf("stamped lanes: got %d; want 3", res.Advice.Lanes)
	}
	one := 1
	if res := f.advice(Options{Lanes: &one}); res.Advice.Lanes != 1 {
		t.Errorf("what-if lanes: got %d; want the what-if 1, not the stamp's 3", res.Advice.Lanes)
	}

	// A newer reading with no usable stamp falls back, newest first, to the older stamped
	// one; a stamped zero is a real count.
	older := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	two, none := 2, 0
	older.laneReading(serviceReadingAt.Add(-48*time.Hour), 20, &two, LanesRecorded)
	older.laneReading(serviceReadingAt, serviceReadingPct, nil, LanesUnavailable)
	if res := older.advice(Options{}); res.Advice.Lanes != 2 {
		t.Errorf("newest reading unstamped: got %d lanes; want the older stamp's 2", res.Advice.Lanes)
	}
	older.laneReading(serviceReadingAt.Add(time.Hour), 41, &none, LanesCurrent)
	res := older.advice(Options{})
	if res.Advice.Lanes != 0 || res.Advice.LanesStamped == nil || *res.Advice.LanesStamped != 0 {
		t.Errorf("newest stamp names 0 lanes; got lanes %d stamped %v", res.Advice.Lanes, res.Advice.LanesStamped)
	}

	// No usable stamp on any reading: the stored maximum plans, then the package default.
	blank := newServiceFixture(t, Settings{Reset: serviceReset, MaxLanes: 4}, nil)
	blank.laneReading(serviceReadingAt, serviceReadingPct, nil, "")
	if res := blank.advice(Options{}); res.Advice.Lanes != 4 {
		t.Errorf("stored maximum: got %d; want 4", res.Advice.Lanes)
	}
	noneBlank := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	noneBlank.laneReading(serviceReadingAt, serviceReadingPct, nil, "")
	if res := noneBlank.advice(Options{}); res.Advice.Lanes != DefaultMaxLanes {
		t.Errorf("package default: got %d; want %d", res.Advice.Lanes, DefaultMaxLanes)
	}
}

func TestServiceAdviceChangesWithNewestStamp(t *testing.T) {
	// Both the divisor of the burn per lane and the default chosen count follow the
	// newest stamp: a newer reading with another count changes the advice.
	f := newServiceFixture(t, Settings{Reset: serviceReset, MaxLanes: 4}, nil)
	two, four := 2, 4
	f.laneReading(serviceReadingAt.Add(-30*time.Hour), 20, &two, LanesLive)
	f.laneReading(serviceReadingAt, 40, &two, LanesLive)
	before := f.advice(Options{})
	if before.Advice.Lanes != 2 || before.Advice.RecentLanes != 2 {
		t.Fatalf("stamp 2: lanes %d recent lanes %d; want 2 and 2", before.Advice.Lanes, before.Advice.RecentLanes)
	}
	if before.Advice.PerLanePer24h <= 0 {
		t.Fatalf("burn per lane is %v; the fixture needs a burn above the idle baseline", before.Advice.PerLanePer24h)
	}

	f.laneReading(serviceReadingAt.Add(time.Hour), 41, &four, LanesRecorded)
	after := f.advice(Options{})
	if after.Advice.Lanes != 4 || after.Advice.RecentLanes != 4 {
		t.Errorf("stamp 4: lanes %d recent lanes %d; want 4 and 4", after.Advice.Lanes, after.Advice.RecentLanes)
	}
	if after.Advice.LanesStamped == nil || *after.Advice.LanesStamped != 4 {
		t.Errorf("LanesStamped: got %v; want 4", after.Advice.LanesStamped)
	}
	if after.Advice.Chosen == nil || after.Advice.Chosen.Lanes != 4 {
		t.Errorf("chosen row: got %+v; want the 4-lane row", after.Advice.Chosen)
	}

	// The divisor alone: a what-if keeps the chosen count, yet the burn per lane still
	// divides by the newest stamp. The same burn over a larger stamp is a smaller share.
	same := newServiceFixture(t, Settings{Reset: serviceReset, MaxLanes: 4}, nil)
	same.laneReading(serviceReadingAt.Add(-30*time.Hour), 20, &two, LanesLive)
	same.laneReading(serviceReadingAt, 40, &two, LanesLive)
	wide := newServiceFixture(t, Settings{Reset: serviceReset, MaxLanes: 4}, nil)
	wide.laneReading(serviceReadingAt.Add(-30*time.Hour), 20, &two, LanesLive)
	wide.laneReading(serviceReadingAt, 40, &four, LanesLive)
	three := 3
	a, b := same.advice(Options{Lanes: &three}), wide.advice(Options{Lanes: &three})
	if a.Advice.Lanes != 3 || b.Advice.Lanes != 3 {
		t.Fatalf("what-if lanes: got %d and %d; want 3 and 3", a.Advice.Lanes, b.Advice.Lanes)
	}
	if !(b.Advice.PerLanePer24h < a.Advice.PerLanePer24h) {
		t.Errorf("burn per lane over stamp 4 (%v) is not smaller than over stamp 2 (%v)", b.Advice.PerLanePer24h, a.Advice.PerLanePer24h)
	}
}

func TestServiceWhatIfZeroDiffersFromUnset(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t) // 3 lanes, live
	zero := 0
	unset, whatIf := f.advice(Options{}), f.advice(Options{Lanes: &zero})
	if unset.Advice.Lanes != 3 {
		t.Errorf("unset: got %d lanes; want the stamp's 3", unset.Advice.Lanes)
	}
	if whatIf.Advice.Lanes != 0 {
		t.Errorf("what-if 0: got %d lanes; want 0 honoured", whatIf.Advice.Lanes)
	}
	if whatIf.Advice.WhatIfLanes == nil || *whatIf.Advice.WhatIfLanes != 0 {
		t.Errorf("WhatIfLanes: got %v; want a set 0", whatIf.Advice.WhatIfLanes)
	}
	if unset.Advice.WhatIfLanes != nil {
		t.Errorf("WhatIfLanes without a what-if: got %v; want nil", *unset.Advice.WhatIfLanes)
	}
	if whatIf.Advice.Chosen == nil || whatIf.Advice.Chosen.Lanes != 0 {
		t.Errorf("chosen row for the what-if: got %+v; want the 0-lane row", whatIf.Advice.Chosen)
	}
	// The stamp and the divisor do not move with the what-if.
	if whatIf.Advice.RecentLanes != 3 || whatIf.Advice.PerLanePer24h != unset.Advice.PerLanePer24h {
		t.Errorf("what-if moved the divisor: recent %d per lane %v; want 3 and %v",
			whatIf.Advice.RecentLanes, whatIf.Advice.PerLanePer24h, unset.Advice.PerLanePer24h)
	}
}

func TestServiceTypedAndUnavailableIgnoredForDivisor(t *testing.T) {
	// Only the typed and unavailable readings carry counts here, so the divisor is nil and
	// the model falls back to the chosen count; the default is the stored maximum.
	f := newServiceFixture(t, Settings{Reset: serviceReset, MaxLanes: 4}, nil)
	seven, five, two := 7, 5, 2
	f.laneReading(serviceReadingAt.Add(-30*time.Hour), 20, &seven, LanesTyped)
	f.laneReading(serviceReadingAt, 40, &five, LanesUnavailable)
	res := f.advice(Options{})
	if res.Advice.LanesStamped != nil {
		t.Errorf("LanesStamped: got %d; want nil for typed and unavailable readings", *res.Advice.LanesStamped)
	}
	if res.Advice.Lanes != 4 || res.Advice.RecentLanes != 4 {
		t.Errorf("lanes %d recent %d; want the stored maximum 4 for both", res.Advice.Lanes, res.Advice.RecentLanes)
	}

	// A stamped reading older than the typed ones is the one that counts.
	f.laneReading(serviceReadingAt.Add(-60*time.Hour), 10, &two, LanesLive)
	res = f.advice(Options{})
	if res.Advice.LanesStamped == nil || *res.Advice.LanesStamped != 2 || res.Advice.RecentLanes != 2 {
		t.Errorf("older stamp: stamped %v recent %d; want 2 and 2", res.Advice.LanesStamped, res.Advice.RecentLanes)
	}
}

func TestServiceLanesRunningNow(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t)
	if got := f.advice(Options{}).Advice.LanesRunningNow; got != nil {
		t.Errorf("without a ledger: got %d; want nil", *got)
	}

	counter := &fakeLaneCounter{count: 2, ok: true}
	f.svc = NewService(f.series, f.store, func() Settings { return Settings{Reset: serviceReset} }, nil,
		WithServiceNow(f.clock.Now), WithLiveLanes(counter))
	res := f.advice(Options{})
	if got := res.Advice.LanesRunningNow; got == nil || *got != 2 {
		t.Errorf("ledger counts 2: got %v; want 2", got)
	}
	// The running count is its own number: it is not the stamp and not the chosen count.
	if res.Advice.LanesStamped == nil || *res.Advice.LanesStamped != 3 || res.Advice.Lanes != 3 {
		t.Errorf("stamped %v lanes %d; want 3 and 3 whatever the ledger counts", res.Advice.LanesStamped, res.Advice.Lanes)
	}
	counter.set(0, true)
	if got := f.advice(Options{}).Advice.LanesRunningNow; got == nil || *got != 0 {
		t.Errorf("ledger counts 0: got %v; want a real 0", got)
	}
	counter.set(0, false)
	res = f.advice(Options{})
	if res.Advice.LanesRunningNow != nil {
		t.Errorf("ledger not wired: got %d; want nil", *res.Advice.LanesRunningNow)
	}
	raw, err := json.Marshal(res.Advice)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := m["lanes_running_now"]; !ok || string(v) != "null" {
		t.Errorf("lanes_running_now in JSON: got %s (present %v); want null", v, ok)
	}
}

func TestServiceLaneNumbersInText(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t)
	f.svc = NewService(f.series, f.store, func() Settings { return Settings{Reset: serviceReset} }, nil,
		WithServiceNow(f.clock.Now), WithLiveLanes(&fakeLaneCounter{count: 2, ok: true}))
	zero := 0
	res := f.advice(Options{Lanes: &zero})
	for name, text := range map[string]string{"cli": res.Text.CLI, "comm": res.Text.Comm} {
		mustContainAll(t, name, text,
			"Claude lanes running now (ledger count): 2",
			"Lanes stamped on the newest reading: 3",
			"What-if lanes (not saved): 0",
		)
	}
}

func TestServiceRemainingWorkTyped(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	ctx := context.Background()

	if f.svc.TypedRemainingWork() != nil {
		t.Errorf("TypedRemainingWork before any figure: want nil")
	}

	pts := 15.0
	if err := f.svc.SetRemainingWork(ctx, &pts); err != nil {
		t.Fatalf("SetRemainingWork: %v", err)
	}
	if got := f.svc.TypedRemainingWork(); got == nil || *got != 15 {
		t.Errorf("TypedRemainingWork after set: got %v; want 15", got)
	}

	// The figure is daemon-local: it survives the service, not a change of keeper record.
	res, err := f.svc.Advice(ctx, Options{Policy: PolicyFinishEarly})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Advice.RemainingWork.InUse != WorkInUseTyped || res.Advice.RemainingWork.Points != 15 {
		t.Errorf("typed work in advice: InUse %q Points %v; want typed/15",
			res.Advice.RemainingWork.InUse, res.Advice.RemainingWork.Points)
	}

	if err := f.svc.SetRemainingWork(ctx, nil); err != nil {
		t.Fatalf("clear remaining work: %v", err)
	}
	if got := f.svc.TypedRemainingWork(); got != nil {
		t.Errorf("TypedRemainingWork after clear: got %v; want nil", got)
	}

	// Invalid figures are refused, and a service without a local store keeps none.
	inf := math.Inf(1)
	if err := f.svc.SetRemainingWork(ctx, &inf); err == nil {
		t.Errorf("SetRemainingWork with +Inf: want an error")
	}
	neg := -1.0
	if err := f.svc.SetRemainingWork(ctx, &neg); err == nil {
		t.Errorf("SetRemainingWork with a negative figure: want an error")
	}
	orphan := NewService(f.series, nil, func() Settings { return Settings{} }, nil)
	if err := orphan.SetRemainingWork(ctx, &pts); err == nil {
		t.Errorf("SetRemainingWork without a local store: want an error")
	}
}

func TestServiceStaleSourceMarked(t *testing.T) {
	// A peer that cannot reach the keeper serves its last copy, marked stale (decision 8).
	clock := newFakeClock(serviceNow)
	store, err := OpenStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	series := NewSharedSeries(store, "keeper-a", func(name string) (string, string, bool) {
		// A registered peer that refuses the connection outright.
		return "http://[::1]:1", "", true
	}, WithSharedClock(clock.Now))

	pct, lanes := serviceReadingPct, serviceReadingLn
	reading := Reading{At: serviceReadingAt, Percent: &pct, Lanes: &lanes, LanesSource: LanesLive, Source: SourceTyped}
	if err := series.writeCopy(peerCopy{CopiedAt: serviceNow.Add(-time.Hour), Keeper: "keeper-a", Record: AccountRecord{
		Readings: []Reading{reading}, Keeper: "keeper-a",
	}}); err != nil {
		t.Fatalf("seed the last copy: %v", err)
	}
	svc := NewService(series, nil, func() Settings { return Settings{Reset: serviceReset} }, nil, WithServiceNow(clock.Now))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("Advice on the stale copy: %v", err)
	}
	if !res.Stale {
		t.Errorf("Stale: got false when the keeper is unreachable; want true")
	}
	if res.StaleReason == "" {
		t.Errorf("StaleReason is empty; the screen must say why the copy is served")
	}
	if res.CopiedAt == nil {
		t.Errorf("CopiedAt is nil; the stamp of the last copy must be shown")
	}
	if math.Abs(res.Advice.Percent-serviceReadingPct) > 1e-9 {
		t.Errorf("Percent from the copy: got %v; want %v", res.Advice.Percent, serviceReadingPct)
	}
	if res.Advice.LatestAgeHours == nil || math.Abs(*res.Advice.LatestAgeHours-76) > 1e-9 {
		t.Errorf("LatestAgeHours from the copy: got %v; want 76", res.Advice.LatestAgeHours)
	}

	// Without a copy at all, the call fails instead of inventing a series. A fresh
	// directory is needed so the seeded copy above does not leak into this peer.
	nakedStore, err := OpenStore(t.TempDir(), clock.Now)
	if err != nil {
		t.Fatalf("OpenStore for the naked peer: %v", err)
	}
	naked := NewSharedSeries(nakedStore, "keeper-a", func(name string) (string, string, bool) {
		return "http://[::1]:9", "", true
	})
	orphan := NewService(naked, nil, func() Settings { return Settings{Reset: serviceReset} }, nil)
	_, err = orphan.Advice(context.Background(), Options{})
	if err == nil {
		t.Fatalf("Advice with no copy and the keeper down: want an error")
	}
	if !strings.Contains(err.Error(), "copy") {
		t.Errorf("error %q; want it to point at the missing local copy", err)
	}
}

func ptr64(v float64) *float64 { return &v }

// TestServiceNoReadings checks the clean, clearly labeled empty state through the
// service: no readings in the series means no percentage is invented. The service tells
// the operator to take the first reading, the queue and policy chains stay usable, and
// the shared record's write paths work before that first reading.
func TestServiceNoReadings(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, fakeQueue{n: 3})
	ctx := context.Background()

	res, err := f.svc.Advice(ctx, Options{Policy: PolicyFinishEarly})
	if err != nil {
		t.Fatalf("Advice with no readings: %v", err)
	}
	if res.Advice.Needs != NeedsReading {
		t.Errorf("Needs: got %q; want %q", res.Advice.Needs, NeedsReading)
	}
	if res.Advice.Percent != 0 || len(res.Advice.Series) != 0 {
		t.Errorf("empty state: percent %v series %d; want 0 and 0", res.Advice.Percent, len(res.Advice.Series))
	}
	if res.Advice.RemainingWork.InUse != WorkInUseNone {
		t.Errorf("InUse: got %q; want %q with no reading", res.Advice.RemainingWork.InUse, WorkInUseNone)
	}

	// The readings pass-through serves the shared series: empty here.
	readings, err := f.svc.Readings(ctx)
	if err != nil {
		t.Fatalf("Readings: %v", err)
	}
	if len(readings) != 0 {
		t.Errorf("Readings: got %d rows; want 0", len(readings))
	}

	// Extra credit is an account fact: it goes in before the first reading, and the
	// advisory ceiling follows it.
	if err := f.svc.SetExtraCredit(ctx, 25); err != nil {
		t.Fatalf("SetExtraCredit: %v", err)
	}
	res, err = f.svc.Advice(ctx, Options{})
	if err != nil {
		t.Fatalf("Advice after extra credit: %v", err)
	}
	if res.Advice.Ceiling != 125 {
		t.Errorf("Ceiling: got %v; want %v", res.Advice.Ceiling, 125)
	}
}
