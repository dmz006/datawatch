package autonomous

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
)

// fakeSpendEntry is one fake spend record: dollars by a member's PRD at a
// time under an LLM.
type fakeSpendEntry struct {
	at     time.Time
	member string
	llm    string
	usd    float64
}

// budgetEvalFixture is an evaluator on a fake clock and a fake spend source.
type budgetEvalFixture struct {
	clk     *budgetFakeClock
	b       *AutomationBudget
	entries []fakeSpendEntry
	lookups int
}

func newBudgetEvalFixture(t *testing.T) *budgetEvalFixture {
	t.Helper()
	f := &budgetEvalFixture{clk: &budgetFakeClock{t: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}}
	f.b = NewAutomationBudget(nil, nil)
	f.b.Clock = f.clk.Now
	f.b.Spend = func(a *Automation, member *AutomationMember, since time.Time, llm string) float64 {
		f.lookups++
		var sum float64
		for _, e := range f.entries {
			if (member != nil && e.member != member.ID) || e.at.Before(since) || (llm != "" && e.llm != llm) {
				continue
			}
			sum += e.usd
		}
		return sum
	}
	return f
}

// spend records usd by member m at at under no particular LLM.
func (f *budgetEvalFixture) spend(m string, at time.Time, usd float64) {
	f.entries = append(f.entries, fakeSpendEntry{at: at, member: m, usd: usd})
}

func dayPolicy(ceiling float64) *BudgetPolicy {
	return &BudgetPolicy{CeilingType: "dollar", CeilingValue: ceiling, Window: "day", ResetZone: "UTC"}
}

func evalAutomation(p *BudgetPolicy, members ...AutomationMember) *Automation {
	started := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return &Automation{ID: "a1", Status: AutomationActive, StartedAt: &started, BudgetPolicy: p, Members: members}
}

func TestBudgetEvalDollarCeilingReached(t *testing.T) {
	f := newBudgetEvalFixture(t)
	a := evalAutomation(dayPolicy(10), AutomationMember{ID: "m1", PRDID: "p1"})
	f.spend("m1", f.clk.Now().Add(-3*time.Hour), 10)

	v := f.b.Evaluate(a, nil, false)
	if !v.Gate || v.Kind != BudgetKindCeiling || v.Scope != "automation" {
		t.Fatalf("verdict = %+v, want a gating ceiling at automation scope", v)
	}
	if v.SpentUSD != 10 || v.CeilingUSD != 10 || v.RemainingUSD != 0 {
		t.Fatalf("spent/ceiling/remaining = %v/%v/%v", v.SpentUSD, v.CeilingUSD, v.RemainingUSD)
	}
	if !v.WindowStart.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) || !v.WindowEnd.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window = %v..%v", v.WindowStart, v.WindowEnd)
	}
	if v.Policy != a.BudgetPolicy || v.Reason == "" || strings.Contains(v.Reason, "BL") {
		t.Fatalf("policy/reason = %v / %q", v.Policy, v.Reason)
	}
	// The same state is a gate in hard mode too.
	if h := f.b.Evaluate(a, nil, true); !h.Gate || h.Kind != BudgetKindCeiling {
		t.Fatalf("hard verdict = %+v", h)
	}
	// With no reset source wired, nothing is known about the reset.
	if v.ResetKnown || v.ResetAt != nil || v.ResetSource != "" {
		t.Fatalf("reset = %v %v %q, want none", v.ResetKnown, v.ResetAt, v.ResetSource)
	}
}

func TestBudgetEvalBelowCeilingAllows(t *testing.T) {
	f := newBudgetEvalFixture(t)
	a := evalAutomation(dayPolicy(10), AutomationMember{ID: "m1", PRDID: "p1"})
	f.spend("m1", f.clk.Now().Add(-time.Hour), 4)

	v := f.b.Evaluate(a, nil, false)
	if v.Gate || v.Kind != "" || v.Reason != "" {
		t.Fatalf("verdict = %+v, want no gate", v)
	}
	if v.SpentUSD != 4 || v.RemainingUSD != 6 {
		t.Fatalf("spent/remaining = %v/%v", v.SpentUSD, v.RemainingUSD)
	}
	// Spend from before the window start does not count.
	f.spend("m1", f.clk.Now().Add(-20*time.Hour), 50)
	if v := f.b.Evaluate(a, nil, false); v.Gate || v.SpentUSD != 4 {
		t.Fatalf("yesterday's spend counted: %+v", v)
	}
}

func TestBudgetEvalNoPolicyZeroVerdict(t *testing.T) {
	f := newBudgetEvalFixture(t)
	a := evalAutomation(nil, AutomationMember{ID: "m1", PRDID: "p1"})
	f.spend("m1", f.clk.Now(), 1000)
	v := f.b.Evaluate(a, &a.Members[0], false)
	if v.Gate || v.Policy != nil || v.SpentUSD != 0 || v.Scope != "" {
		t.Fatalf("verdict = %+v, want zero", v)
	}
	if f.lookups != 0 {
		t.Fatalf("%d spend lookups with no policy", f.lookups)
	}
}

func TestBudgetEvalTightestWins(t *testing.T) {
	f := newBudgetEvalFixture(t)
	m1 := AutomationMember{ID: "m1", PRDID: "p1", BudgetPolicy: dayPolicy(10)}
	m2 := AutomationMember{ID: "m2", PRDID: "p2"}
	a := evalAutomation(dayPolicy(50), m1, m2)
	now := f.clk.Now()

	// Only the member policy gates: m1 is at its $10, the Automation is at $15 of $50.
	f.spend("m1", now.Add(-time.Hour), 10)
	f.spend("m2", now.Add(-time.Hour), 5)
	v := f.b.Evaluate(a, &a.Members[0], false)
	if !v.Gate || v.Scope != "member:m1" || v.CeilingUSD != 10 || v.SpentUSD != 10 {
		t.Fatalf("verdict = %+v, want a gate at member:m1", v)
	}

	// Both gate: the Automation is $5 over, m1 is $2 over; the Automation has the least remaining.
	f.spend("m1", now.Add(-time.Minute), 2)
	f.spend("m2", now.Add(-time.Minute), 38)
	v = f.b.Evaluate(a, &a.Members[0], false)
	if !v.Gate || v.Scope != "automation" || v.SpentUSD != 55 || v.CeilingUSD != 50 {
		t.Fatalf("verdict = %+v, want a gate at automation scope", v)
	}

	// m2 has no policy of its own, so the Automation policy alone decides for it.
	v = f.b.Evaluate(a, &a.Members[1], false)
	if !v.Gate || v.Scope != "automation" {
		t.Fatalf("m2 verdict = %+v", v)
	}
}

func TestBudgetEvalMemberOverrideNarrowsAutomation(t *testing.T) {
	f := newBudgetEvalFixture(t)
	// The member asks for $50, wider than the Automation's $10: it cannot widen it.
	wide := AutomationMember{ID: "m1", PRDID: "p1", BudgetPolicy: dayPolicy(50)}
	a := evalAutomation(dayPolicy(10), wide, AutomationMember{ID: "m2", PRDID: "p2"})
	now := f.clk.Now()
	f.spend("m2", now.Add(-time.Hour), 12)

	v := f.b.Evaluate(a, &a.Members[0], false)
	if !v.Gate || v.Scope != "automation" || v.CeilingUSD != 10 {
		t.Fatalf("verdict = %+v, want the Automation ceiling to gate", v)
	}

	// A narrower member policy gates even though the Automation has room.
	a2 := evalAutomation(dayPolicy(100), AutomationMember{ID: "m1", PRDID: "p1", BudgetPolicy: dayPolicy(3)}, AutomationMember{ID: "m2", PRDID: "p2"})
	f.spend("m1", now.Add(-time.Hour), 3)
	v = f.b.Evaluate(a2, &a2.Members[0], false)
	if !v.Gate || v.Scope != "member:m1" || v.CeilingUSD != 3 {
		t.Fatalf("narrow verdict = %+v, want member:m1 to gate", v)
	}
}

func TestBudgetEvalPacingEvenHoldsStart(t *testing.T) {
	f := newBudgetEvalFixture(t)
	f.clk.t = time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC) // a quarter of the day
	p := dayPolicy(24)
	p.Pacing = "even"
	a := evalAutomation(p, AutomationMember{ID: "m1", PRDID: "p1"})
	f.spend("m1", f.clk.Now().Add(-time.Hour), 8)

	v := f.b.Evaluate(a, &a.Members[0], false)
	if !v.Gate || v.Kind != BudgetKindPacing || v.PacingAllowedUSD != 6 {
		t.Fatalf("verdict = %+v, want pacing to hold with $6 allowed", v)
	}
	if v.SpentUSD != 8 || v.CeilingUSD != 24 {
		t.Fatalf("spent/ceiling = %v/%v", v.SpentUSD, v.CeilingUSD)
	}
	// Hard mode ignores pacing.
	if h := f.b.Evaluate(a, &a.Members[0], true); h.Gate {
		t.Fatalf("hard verdict gated on pacing: %+v", h)
	}
	// Once the day catches up with the spend the hold lifts.
	f.clk.Advance(6 * time.Hour)
	if v := f.b.Evaluate(a, &a.Members[0], false); v.Gate || v.PacingAllowedUSD != 12 {
		t.Fatalf("verdict at noon = %+v, want the hold lifted", v)
	}
}

func TestBudgetEvalPacingNoneSpendsToCeiling(t *testing.T) {
	f := newBudgetEvalFixture(t)
	f.clk.t = time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)
	for _, pacing := range []string{"none", ""} {
		p := dayPolicy(24)
		p.Pacing = pacing
		a := evalAutomation(p, AutomationMember{ID: "m1", PRDID: "p1"})
		f.entries = nil
		f.spend("m1", f.clk.Now().Add(-time.Hour), 20)
		v := f.b.Evaluate(a, nil, false)
		if v.Gate || v.PacingAllowedUSD != 0 {
			t.Fatalf("pacing %q: verdict = %+v, want free spending", pacing, v)
		}
		f.spend("m1", f.clk.Now(), 4)
		if v := f.b.Evaluate(a, nil, false); !v.Gate || v.Kind != BudgetKindCeiling {
			t.Fatalf("pacing %q: verdict at ceiling = %+v", pacing, v)
		}
	}

	// Pacing is ignored for the lifetime window: it has no elapsed fraction.
	p := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "automation", Pacing: "even"}
	a := evalAutomation(p, AutomationMember{ID: "m1", PRDID: "p1"})
	started := f.clk.Now().Add(-time.Minute)
	a.StartedAt = &started
	f.entries = nil
	f.spend("m1", f.clk.Now(), 9)
	if v := f.b.Evaluate(a, nil, false); v.Gate || v.PacingAllowedUSD != 0 || !v.WindowEnd.IsZero() {
		t.Fatalf("lifetime verdict = %+v, want no pacing and no window end", v)
	}
}

func TestBudgetEvalMemberEstimateWouldCrossCeiling(t *testing.T) {
	f := newBudgetEvalFixture(t)
	now := f.clk.Now()
	a := evalAutomation(dayPolicy(10),
		AutomationMember{ID: "m1", PRDID: "p1", EstimatedCostUSD: 4},
		AutomationMember{ID: "m2", PRDID: "p2"})
	f.spend("m2", now.Add(-time.Hour), 7)

	v := f.b.Evaluate(a, &a.Members[0], false)
	if !v.Gate || v.Kind != BudgetKindEstimate || v.EstimateUSD != 4 || v.EstimateSource != BudgetEstimateDeclared {
		t.Fatalf("verdict = %+v, want an estimate gate of $4", v)
	}
	if !strings.Contains(v.Reason, "$4.00") || strings.Contains(v.Reason, "BL") {
		t.Fatalf("reason = %q", v.Reason)
	}
	// Hard mode does not use the estimate.
	if h := f.b.Evaluate(a, &a.Members[0], true); h.Gate {
		t.Fatalf("hard verdict gated on an estimate: %+v", h)
	}
	// An estimate that lands exactly on the ceiling does not cross it.
	a.Members[0].EstimatedCostUSD = 3
	if v := f.b.Evaluate(a, &a.Members[0], false); v.Gate {
		t.Fatalf("verdict = %+v, want a start that lands on the ceiling", v)
	}

	// With no declared estimate, a resumed PRD's own earlier spend stands in.
	a.Members[0].EstimatedCostUSD = 0
	f.spend("m1", now.Add(-30*time.Hour), 5) // before today's window, still m1's earlier spend
	v = f.b.Evaluate(a, &a.Members[0], false)
	if !v.Gate || v.Kind != BudgetKindEstimate || v.EstimateUSD != 5 || v.EstimateSource != BudgetEstimatePriorSpend {
		t.Fatalf("resumed verdict = %+v, want an estimate gate from prior spend", v)
	}

	// For the lifetime window waiting can never help, so it is reported as the ceiling.
	life := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "automation"}
	al := evalAutomation(life, AutomationMember{ID: "m1", PRDID: "p1", EstimatedCostUSD: 6}, AutomationMember{ID: "m2", PRDID: "p2"})
	f.entries = nil
	f.spend("m2", now.Add(-time.Hour), 7)
	v = f.b.Evaluate(al, &al.Members[0], false)
	if !v.Gate || v.Kind != BudgetKindCeiling || v.EstimateUSD != 6 {
		t.Fatalf("lifetime verdict = %+v, want a ceiling gate", v)
	}
}

func TestBudgetEvalNoEstimateStartsBelowCeiling(t *testing.T) {
	f := newBudgetEvalFixture(t)
	now := f.clk.Now()
	a := evalAutomation(dayPolicy(10), AutomationMember{ID: "m1", PRDID: "p1"}, AutomationMember{ID: "m2", PRDID: "p2"})
	f.spend("m2", now.Add(-time.Hour), 9.5)

	v := f.b.Evaluate(a, &a.Members[0], false)
	if v.Gate || v.EstimateUSD != 0 || v.EstimateSource != "" {
		t.Fatalf("verdict = %+v, want a start with no estimate", v)
	}
	if v.RemainingUSD != 0.5 {
		t.Fatalf("remaining = %v", v.RemainingUSD)
	}
}

// allocationFixture gives the evaluator a plan tier on "claude" with a
// notional $100 allocation, through a real ledger.
func allocationFixture(t *testing.T) *budgetEvalFixture {
	t.Helper()
	f := newBudgetEvalFixture(t)
	led := newTestBudgetLedger(t, f.clk, &inference.BudgetWindow{CeilingUSD: 1000, Hours: 5})
	led.PlanTier = func(llm string) inference.PlanTier {
		if llm == "claude" {
			return inference.PlanTierMax5x
		}
		return inference.PlanTierUnset
	}
	led.PlanTierAllocations = func() map[string]float64 { return map[string]float64{string(inference.PlanTierMax5x): 100} }
	f.b.ledger = led
	return f
}

func TestBudgetEvalAllocationEstimateNeverBlocks(t *testing.T) {
	f := allocationFixture(t)
	now := f.clk.Now()
	f.entries = append(f.entries, fakeSpendEntry{at: now.Add(-time.Hour), member: "m1", llm: "claude", usd: 500})

	// An allocation_pct policy only fills the estimate, far above 100 percent.
	pct := &BudgetPolicy{CeilingType: "allocation_pct", CeilingValue: 1, Window: "day", ResetZone: "UTC", Backend: "claude"}
	a := evalAutomation(pct, AutomationMember{ID: "m1", PRDID: "p1"})
	v := f.b.Evaluate(a, &a.Members[0], false)
	if v.Gate || v.Kind != "" {
		t.Fatalf("allocation_pct verdict gated: %+v", v)
	}
	if v.Allocation == nil || !v.Allocation.Estimated || v.Allocation.AllocationPct != 500 || v.Allocation.AllocationUSD != 100 || v.Allocation.PlanTier != "max_5x" {
		t.Fatalf("allocation = %+v, want an estimated 500%% of $100", v.Allocation)
	}
	if v.CeilingUSD != 0 {
		t.Fatalf("allocation_pct set a dollar ceiling of %v", v.CeilingUSD)
	}
	// Hard mode has nothing to evaluate for it.
	if h := f.b.Evaluate(a, &a.Members[0], true); h.Gate || h.Allocation != nil {
		t.Fatalf("hard verdict = %+v", h)
	}

	// An opted-in dollar policy reports the figure without letting it gate.
	dollar := dayPolicy(1000)
	dollar.Backend = "claude"
	dollar.AllocationEstimate = true
	a = evalAutomation(dollar, AutomationMember{ID: "m1", PRDID: "p1"})
	v = f.b.Evaluate(a, &a.Members[0], false)
	if v.Gate || v.Allocation == nil || v.Allocation.AllocationPct != 500 || !v.Allocation.Estimated {
		t.Fatalf("opted-in verdict = %+v", v)
	}

	// An allocation_pct member policy next to a dollar Automation policy still never gates.
	a = evalAutomation(dayPolicy(1000), AutomationMember{ID: "m1", PRDID: "p1", BudgetPolicy: pct})
	if v := f.b.Evaluate(a, &a.Members[0], false); v.Gate || v.Allocation == nil {
		t.Fatalf("mixed verdict = %+v", v)
	}
}

func TestBudgetEvalAllocationEstimateOffByDefault(t *testing.T) {
	f := allocationFixture(t)
	f.entries = append(f.entries, fakeSpendEntry{at: f.clk.Now().Add(-time.Hour), member: "m1", llm: "claude", usd: 5})

	dollar := dayPolicy(100)
	dollar.Backend = "claude"
	a := evalAutomation(dollar, AutomationMember{ID: "m1", PRDID: "p1"})
	if v := f.b.Evaluate(a, &a.Members[0], false); v.Allocation != nil {
		t.Fatalf("allocation filled without opting in: %+v", v.Allocation)
	}

	// Opted in but the plan tier has no declared allocation: still nothing.
	dollar.AllocationEstimate = true
	f.b.ledger.PlanTierAllocations = func() map[string]float64 { return nil }
	if v := f.b.Evaluate(a, &a.Members[0], false); v.Allocation != nil {
		t.Fatalf("allocation filled without a declared allocation: %+v", v.Allocation)
	}

	// Opted in with a declared allocation, but the admit hook's hard mode never reports it.
	f.b.ledger.PlanTierAllocations = func() map[string]float64 { return map[string]float64{"max_5x": 100} }
	if v := f.b.Evaluate(a, &a.Members[0], true); v.Allocation != nil {
		t.Fatalf("hard mode filled the allocation: %+v", v.Allocation)
	}
	if v := f.b.Evaluate(a, &a.Members[0], false); v.Allocation == nil || v.Allocation.AllocationPct != 5 {
		t.Fatalf("opted-in allocation = %+v, want 5%%", v.Allocation)
	}
}

func TestBudgetEvalWindowRolloverClearsBreach(t *testing.T) {
	f := newBudgetEvalFixture(t)
	a := evalAutomation(dayPolicy(10), AutomationMember{ID: "m1", PRDID: "p1"})
	f.spend("m1", f.clk.Now().Add(-time.Hour), 10)
	if v := f.b.Evaluate(a, nil, false); !v.Gate {
		t.Fatalf("verdict = %+v, want a breach before the boundary", v)
	}
	f.clk.t = time.Date(2026, 10, 22, 23, 59, 0, 0, time.UTC)
	if v := f.b.Evaluate(a, nil, false); !v.Gate {
		t.Fatalf("verdict = %+v, want a breach one minute before the boundary", v)
	}
	f.clk.Advance(2 * time.Minute) // 00:01 the next day
	v := f.b.Evaluate(a, nil, false)
	if v.Gate || v.SpentUSD != 0 {
		t.Fatalf("verdict = %+v, want the breach cleared past the day boundary", v)
	}
	if !v.WindowStart.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window start = %v", v.WindowStart)
	}
}

func TestBudgetEvalUsesInjectedClock(t *testing.T) {
	f := newBudgetEvalFixture(t)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}

	day := dayPolicy(10)
	day.ResetTime = "17:00"
	day.ResetZone = "America/New_York"
	a := evalAutomation(day, AutomationMember{ID: "m1", PRDID: "p1"})
	f.clk.t = time.Date(2026, 10, 22, 12, 0, 0, 0, ny) // before today's anchor: the window began yesterday
	if v := f.b.Evaluate(a, nil, false); !v.WindowStart.Equal(time.Date(2026, 10, 21, 17, 0, 0, 0, ny)) || !v.WindowEnd.Equal(time.Date(2026, 10, 22, 17, 0, 0, 0, ny)) {
		t.Fatalf("morning window = %v..%v", v.WindowStart, v.WindowEnd)
	}
	f.clk.t = time.Date(2026, 10, 22, 18, 0, 0, 0, ny)
	if v := f.b.Evaluate(a, nil, false); !v.WindowStart.Equal(time.Date(2026, 10, 22, 17, 0, 0, 0, ny)) {
		t.Fatalf("evening window start = %v", v.WindowStart)
	}

	// Week and month anchors, with their defaults.
	week := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "week", ResetZone: "UTC"}
	s, e, ok := policyWindow(week, a, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) // a Thursday
	if !ok || !s.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) || !e.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("default week window = %v..%v", s, e)
	}
	week.ResetWeekday, week.ResetTime = "friday", "09:30"
	s, e, _ = policyWindow(week, a, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	if !s.Equal(time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)) || !e.Equal(time.Date(2026, 10, 23, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("friday week window = %v..%v", s, e)
	}
	month := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "month", ResetZone: "UTC"}
	s, e, _ = policyWindow(month, a, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	if !s.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || !e.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("default month window = %v..%v", s, e)
	}
	month.ResetDay = 25
	s, e, _ = policyWindow(month, a, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	if !s.Equal(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)) || !e.Equal(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day-25 month window = %v..%v", s, e)
	}

	// The lifetime window starts when the Automation started and never ends.
	life := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "automation"}
	s, _, ok = policyWindow(life, a, f.clk.Now())
	if ok || !s.Equal(*a.StartedAt) {
		t.Fatalf("lifetime window start = %v, has end = %v", s, ok)
	}
}

func TestBudgetEvalResetAndPeerSeams(t *testing.T) {
	f := newBudgetEvalFixture(t)
	now := f.clk.Now()
	p := dayPolicy(10)
	p.PeerWindow = []string{"edge"}
	a := evalAutomation(p, AutomationMember{ID: "m1", PRDID: "p1"})
	f.spend("m1", now.Add(-time.Hour), 6)

	// Without the seams nothing is added.
	f.b.PeerSpend = nil
	if v := f.b.Evaluate(a, nil, false); v.Gate || v.SpentUSD != 6 {
		t.Fatalf("verdict = %+v", v)
	}

	// The seams add opted-in peer spend and report what is known about the reset.
	f.b.PeerSpend = func(pol *BudgetPolicy, from, to time.Time) (float64, []PeerBudgetStatus) {
		if pol != p || !from.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) || !to.Equal(now) {
			t.Errorf("peer spend asked for %v..%v", from, to)
		}
		return 5, []PeerBudgetStatus{{Peer: "edge", SpendUSD: 5, Counted: true}}
	}
	next := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	f.b.Resets = func(pol *BudgetPolicy, win BudgetWindowSpan, at time.Time) ResetInfo {
		if !win.HasEnd || !win.End.Equal(next) || !at.Equal(now) {
			t.Errorf("resets asked with %+v at %v", win, at)
		}
		return ResetInfo{At: &next, Known: true, Source: BudgetResetWindow}
	}
	v := f.b.Evaluate(a, nil, false)
	if !v.Gate || v.SpentUSD != 11 || v.PeerSpendUSD != 5 || len(v.Peers) != 1 {
		t.Fatalf("verdict = %+v, want peer spend counted", v)
	}
	if !v.ResetKnown || v.ResetAt == nil || !v.ResetAt.Equal(next) || v.ResetSource != BudgetResetWindow {
		t.Fatalf("reset = %v %v %q", v.ResetKnown, v.ResetAt, v.ResetSource)
	}
}
