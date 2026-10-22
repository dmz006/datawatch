package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// gateHarness is a coordinator harness whose clock is shared with the budget
// evaluator, so one fake clock decides both the budget window and the
// coordinator's tick.
type gateHarness struct {
	*coordHarness
	f *budgetEvalFixture
}

// newGateHarness builds a harness over n approved PRDs with a budget evaluator
// on a fake clock and the built-in reset rules. A nil f makes a fresh fixture.
func newGateHarness(t *testing.T, n int, f *budgetEvalFixture) *gateHarness {
	t.Helper()
	if f == nil {
		f = newBudgetEvalFixture(t)
	}
	h := newCoordHarness(t, n)
	f.b.Resets = f.b.ResetFor
	h.m.SetAutomationBudget(f.b)
	h.c.now = f.clk.Now
	h.c.idleTick = 24 * time.Hour // the tests drive the tick by hand
	h.c.Start()
	t.Cleanup(h.c.Stop)
	return &gateHarness{coordHarness: h, f: f}
}

// create makes a chain Automation over the first n PRDs (m0 -> m1 -> ...) with
// the Automation-level policy p.
func (g *gateHarness) create(p *BudgetPolicy, n int) *Automation {
	g.t.Helper()
	members := make([]AutomationMember, n)
	for i := range members {
		members[i] = AutomationMember{ID: "m" + string(rune('0'+i)), PRDID: g.prds[i].ID}
		if i+1 < n {
			members[i].OnComplete = "m" + string(rune('1'+i))
		}
	}
	return g.createMembers(p, members, "")
}

func (g *gateHarness) createMembers(p *BudgetPolicy, members []AutomationMember, entry string) *Automation {
	g.t.Helper()
	a, err := g.m.CreateAutomation(AutomationInput{Name: "gated", EntryMemberID: entry, Members: members, BudgetPolicy: p}, "operator")
	if err != nil {
		g.t.Fatal(err)
	}
	return a
}

// activateRunning makes a active with its entry member running, started two
// days before the fake clock so spend entries fall inside its lifetime.
func (g *gateHarness) activateRunning(a *Automation) *Automation {
	g.t.Helper()
	out, err := g.m.automations.Update(a.ID, 0, func(a *Automation) error {
		started := g.f.clk.Now().Add(-48 * time.Hour)
		a.Status = AutomationActive
		a.StartedAt = &started
		addCursor(a, a.EntryMemberID)
		entry := automationMember(a, a.EntryMemberID)
		entry.Status = MemberRunning
		entry.StartedAt = &started
		return nil
	})
	if err != nil {
		g.t.Fatal(err)
	}
	return out
}

// overCeiling records spend under claude one hour before the fake now.
func (g *gateHarness) overCeiling(usd float64) {
	g.f.entries = append(g.f.entries, fakeSpendEntry{at: g.f.clk.Now().Add(-time.Hour), member: "m0", llm: "claude", usd: usd})
}

// finishFirst ends the entry member, which makes the coordinator advance.
func (g *gateHarness) finishFirst() {
	g.t.Helper()
	g.setStatus(0, PRDCompleted)
	g.event(0, PRDCompleted)
}

func (g *gateHarness) noStarts() {
	g.t.Helper()
	if runs, resumes := g.fake.counts(); len(runs) != 0 || len(resumes) != 0 {
		g.t.Fatalf("starter was called: runs=%v resumes=%v", runs, resumes)
	}
}

func (g *gateHarness) auditCount(action string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, a := range g.audits {
		if a == action {
			n++
		}
	}
	return n
}

func (g *gateHarness) lastAudit(action string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := len(g.audits) - 1; i >= 0; i-- {
		if g.audits[i] == action {
			return g.auditD[i]
		}
	}
	return nil
}

func (g *gateHarness) decisionCount(a *Automation, kind string) int {
	n := 0
	for _, d := range g.get(a).Decisions {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

func claudeDay(ceiling float64) *BudgetPolicy {
	p := dayPolicy(ceiling)
	p.Backend = "claude"
	return p
}

func claudeLifetime(ceiling float64) *BudgetPolicy {
	return &BudgetPolicy{CeilingType: "dollar", CeilingValue: ceiling, Window: "automation", Backend: "claude"}
}

// pastMidnight moves the fake clock to just after the next UTC midnight, the
// end of the day window the clock started in.
func (g *gateHarness) pastMidnight() {
	g.f.clk.t = time.Date(2026, 10, 23, 0, 5, 0, 0, time.UTC)
}

func TestBudgetGateRefusesMemberStartAtCeiling(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	a := g.activateRunning(g.create(claudeDay(10), 2))
	g.overCeiling(12)

	g.finishFirst()

	g.noStarts()
	got := g.get(a)
	if got.Status != AutomationPaused || got.CurrentMemberID != "m1" {
		t.Fatalf("status=%s cursor=%q, want paused on m1", got.Status, got.CurrentMemberID)
	}
	if m1 := got.Members[1]; !memberUnstarted(&m1) || m1.StartedAt != nil {
		t.Fatalf("refused member was marked started: %+v", m1)
	}
	if m0 := got.Members[0]; m0.Status != MemberCompleted || m0.EdgeTaken != "on_complete" {
		t.Fatalf("finished member not recorded: %+v", m0)
	}
	bs := got.BudgetState
	if bs == nil || bs.Action != BudgetActionPaused || bs.Scope != "automation" || bs.SpentUSD != 12 || bs.CeilingUSD != 10 ||
		bs.Window != "day" || !bs.ResetKnown || bs.ResumeAt == nil || bs.BreachedAt.IsZero() {
		t.Fatalf("budget state = %+v", bs)
	}
	if got.IdleSince == nil {
		t.Fatal("IdleSince not set on a gated cursor")
	}
	if p, _ := g.m.store.GetPRD(g.prds[1].ID); p.Status != PRDApproved {
		t.Fatalf("refused member PRD moved to %s", p.Status)
	}
}

func TestBudgetGateRefusesActivateEntryMember(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	a := g.create(claudeDay(10), 2)
	g.overCeiling(12)

	got, err := g.c.activate(a.ID, "operator", 0)
	if err != nil {
		t.Fatal(err)
	}

	g.noStarts()
	if got.Status != AutomationPaused || got.CurrentMemberID != "m0" {
		t.Fatalf("status=%s cursor=%q, want paused on the entry member", got.Status, got.CurrentMemberID)
	}
	if m0 := got.Members[0]; !memberUnstarted(&m0) || m0.StartedAt != nil {
		t.Fatalf("entry member was marked started: %+v", m0)
	}
	if got.BudgetState == nil || got.BudgetState.Action != BudgetActionPaused || !got.BudgetState.ResetKnown {
		t.Fatalf("budget state = %+v", got.BudgetState)
	}
	if g.alertCount("warn|Automation paused") != 1 || g.auditCount("automation_budget_breach") != 1 {
		t.Fatalf("alerts=%v audits=%v", g.alerts, g.audits)
	}
	if !g.hasAudit("automation_activate") {
		t.Fatalf("activation not audited: %v", g.audits)
	}
}

func TestBudgetGateDefersForkAtCeiling(t *testing.T) {
	g := newGateHarness(t, 4, nil)
	st := newBranchStarter()
	g.c.starter = st
	a := g.activateRunning(g.createMembers(claudeDay(10), g.forkMembers(BranchAllMustSucceed, ""), "e0"))
	g.overCeiling(12)

	g.startFork(a)

	if got := st.started(); len(got) != 0 {
		t.Fatalf("branches started at the ceiling: %v", got)
	}
	got := g.get(a)
	if got.Status != AutomationPaused {
		t.Fatalf("status = %s, want paused", got.Status)
	}
	if f := g.member(a, "f1"); !memberUnstarted(&f) || f.FinishedAt != nil {
		t.Fatalf("fork fired despite the gate: %+v", f)
	}
	if j := g.member(a, "j1"); j.Status == MemberRunning {
		t.Fatalf("join armed for a fork that never fired: %+v", j)
	}
	if !isLiveCursor(got, "f1") {
		t.Fatalf("cursors = %v, want the fork held as the cursor", got.ActiveCursors)
	}
	if g.auditCount("automation_fork") != 0 {
		t.Fatalf("fork audited as fired: %v", g.audits)
	}

	// Once the window rolls over the whole fork fires, branches in sorted order.
	g.pastMidnight()
	g.c.checkBudget()
	if want := []string{g.prdID(1), g.prdID(2)}; len(st.started()) != 2 || st.started()[0] != want[0] || st.started()[1] != want[1] {
		t.Fatalf("starts after rollover = %v, want %v", st.started(), want)
	}
	after := g.get(a)
	if after.Status != AutomationActive || after.BudgetState != nil || isLiveCursor(after, "f1") {
		t.Fatalf("after rollover: status=%s state=%+v cursors=%v", after.Status, after.BudgetState, after.ActiveCursors)
	}
	if f := g.member(a, "f1"); f.Status != MemberCompleted || f.Outcome != "forked" {
		t.Fatalf("fork not fired after rollover: %+v", f)
	}
}

func TestBudgetBreachPausesAndAutoResumesAtKnownReset(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	a := g.activateRunning(g.create(claudeDay(10), 2))
	g.overCeiling(12)
	g.finishFirst()

	paused := g.get(a)
	if paused.Status != AutomationPaused || paused.BudgetState == nil {
		t.Fatalf("not paused: %s %+v", paused.Status, paused.BudgetState)
	}
	wantReset := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if !paused.BudgetState.ResumeAt.Equal(wantReset) || paused.BudgetState.ResetSource != BudgetResetWindow {
		t.Fatalf("resume at %v source %q, want %v window", paused.BudgetState.ResumeAt, paused.BudgetState.ResetSource, wantReset)
	}
	if g.alertCount("warn|Automation paused") != 1 {
		t.Fatalf("alerts = %v", g.alerts)
	}
	d := g.lastAudit("automation_budget_breach")
	if d == nil || d["automation_id"] != a.ID || d["action"] != BudgetActionPaused || d["scope"] != "automation" ||
		d["spent_usd"] != 12.0 || d["ceiling_usd"] != 10.0 || d["window"] != "day" ||
		d["reset_at"] != "2026-10-23T00:00:00Z" || d["reset_known"] != true || d["actor"] != coordinatorActor || d["reason"] == "" {
		t.Fatalf("breach audit = %v", d)
	}

	// Before the boundary the tick leaves the pause alone.
	g.f.clk.Advance(6 * time.Hour)
	g.c.checkBudget()
	g.noStarts()
	if got := g.get(a); got.Status != AutomationPaused {
		t.Fatalf("resumed before the window reset: %s", got.Status)
	}

	// Past the boundary the Automation resumes by itself and the member starts.
	g.pastMidnight()
	g.c.checkBudget()

	runs, _ := g.fake.counts()
	if len(runs) != 1 || runs[0] != g.prds[1].ID {
		t.Fatalf("runs = %v, want the held member started once", runs)
	}
	got := g.get(a)
	if got.Status != AutomationActive || got.BudgetState != nil || got.IdleSince != nil {
		t.Fatalf("after resume: status=%s state=%+v idle=%v", got.Status, got.BudgetState, got.IdleSince)
	}
	if m1 := got.Members[1]; m1.Status != MemberRunning || m1.StartedAt == nil {
		t.Fatalf("resumed member not running: %+v", m1)
	}
	var resumed *Decision
	for i := range got.Decisions {
		if got.Decisions[i].Kind == controlResume {
			resumed = &got.Decisions[i]
		}
	}
	if resumed == nil || resumed.Actor != budgetActor {
		t.Fatalf("resume decision = %+v, want actor %q", resumed, budgetActor)
	}
	rd := g.lastAudit("automation_budget_resumed")
	if rd == nil || rd["automation_id"] != a.ID || rd["actor"] != budgetActor || rd["action"] != BudgetActionPaused {
		t.Fatalf("resumed audit = %v", rd)
	}
	if g.alertCount("info|Automation resumed") != 1 {
		t.Fatalf("alerts = %v, want one info resume alert", g.alerts)
	}
}

func TestBudgetBreachBlocksAtUnknownReset(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	a := g.activateRunning(g.create(claudeLifetime(10), 2))
	g.overCeiling(12)
	g.finishFirst()

	g.noStarts()
	got := g.get(a)
	if got.Status != AutomationBlocked || got.CurrentMemberID != "m1" {
		t.Fatalf("status=%s cursor=%q, want blocked on m1", got.Status, got.CurrentMemberID)
	}
	bs := got.BudgetState
	if bs == nil || bs.Action != BudgetActionBlocked || bs.ResetKnown || bs.ResumeAt != nil || bs.Window != "automation" {
		t.Fatalf("budget state = %+v", bs)
	}
	if !strings.Contains(bs.Reason, "budget reached") || !strings.Contains(bs.Reason, "operator") || !strings.Contains(bs.Reason, "not known") {
		t.Fatalf("reason does not say what was crossed and who decides: %q", bs.Reason)
	}
	if g.alertCount("warn|Automation blocked") != 1 || g.alertCount("") != 1 {
		t.Fatalf("alerts = %v, want exactly one blocked alert", g.alerts)
	}
	if d := g.lastAudit("automation_budget_breach"); d == nil || d["action"] != BudgetActionBlocked || d["reset_known"] != false {
		t.Fatalf("breach audit = %v", d)
	}

	// No clock movement resumes it.
	g.f.clk.Advance(30 * 24 * time.Hour)
	g.c.checkBudget()
	g.c.checkBudget()
	g.noStarts()
	if got := g.get(a); got.Status != AutomationBlocked {
		t.Fatalf("blocked Automation resumed by itself: %s", got.Status)
	}
}

func TestBudgetBreachOnBreachBlockAlwaysBlocks(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	p := claudeDay(10)
	p.OnBreach = "block"
	a := g.activateRunning(g.create(p, 2))
	g.overCeiling(12)
	g.finishFirst()

	g.noStarts()
	got := g.get(a)
	if got.Status != AutomationBlocked || got.BudgetState == nil || got.BudgetState.Action != BudgetActionBlocked {
		t.Fatalf("status=%s state=%+v, want blocked even though the reset is known", got.Status, got.BudgetState)
	}
	if !got.BudgetState.ResetKnown || !strings.Contains(got.BudgetState.Reason, "on_breach is block") {
		t.Fatalf("state = %+v", got.BudgetState)
	}
	g.pastMidnight()
	g.c.checkBudget()
	g.noStarts()
	if got := g.get(a); got.Status != AutomationBlocked {
		t.Fatalf("on_breach block resumed by itself: %s", got.Status)
	}
}

func TestBudgetBreachAlertsAndAuditsOncePerEpisode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy *BudgetPolicy
		status AutomationStatus
		alert  string
	}{
		{"pause", claudeDay(10), AutomationPaused, "warn|Automation paused"},
		{"block", claudeLifetime(10), AutomationBlocked, "warn|Automation blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGateHarness(t, 2, nil)
			a := g.activateRunning(g.create(tc.policy, 2))
			g.overCeiling(12)
			g.finishFirst()
			before := g.get(a)

			// Replays: the same end event again, ticks, boot reconcile, and a
			// second coordinator over the same store as after a restart.
			g.event(0, PRDCompleted)
			g.c.checkBudget()
			g.c.checkBudget()
			g.c.reconcile()
			g.c.reconcile()
			c2 := NewCoordinator(g.m, g.fake)
			c2.now = g.f.clk.Now
			c2.alertFn, c2.auditFn = g.c.alertFn, g.c.auditFn
			c2.reconcile()
			c2.checkBudget()

			g.noStarts()
			if n := g.alertCount(tc.alert); n != 1 {
				t.Fatalf("%s alerts = %d: %v", tc.name, n, g.alerts)
			}
			if n := g.alertCount(""); n != 1 {
				t.Fatalf("total alerts = %d: %v", n, g.alerts)
			}
			if n := g.auditCount("automation_budget_breach"); n != 1 {
				t.Fatalf("breach audits = %d: %v", n, g.audits)
			}
			after := g.get(a)
			if after.Status != tc.status || after.Version != before.Version || !after.BudgetState.BreachedAt.Equal(before.BudgetState.BreachedAt) {
				t.Fatalf("a replay rewrote the episode: status=%s version %d->%d", after.Status, before.Version, after.Version)
			}
		})
	}
}

func TestBudgetAutoResumeSkippedWhenStillOverCeiling(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	bo := NewBackoff()
	bo.Clock = g.f.clk.Now
	g.f.b.Backoffs = bo.Live
	// A lifetime ceiling is a known reset only while the backend is backed off
	// until a reset time parsed from a rate-limit message.
	resetAt := g.f.clk.Now().Add(2 * time.Hour)
	bo.Set("claude", resetAt, "rate-limit — reset 14:00", BackoffSourceRateLimit)

	a := g.activateRunning(g.create(claudeLifetime(10), 2))
	g.overCeiling(12)
	g.finishFirst()
	paused := g.get(a)
	if paused.Status != AutomationPaused || paused.BudgetState == nil || !paused.BudgetState.ResetKnown ||
		paused.BudgetState.ResetSource != BudgetResetRateLimit || !paused.BudgetState.ResumeAt.Equal(resetAt) {
		t.Fatalf("status=%s state=%+v, want paused until the parsed reset", paused.Status, paused.BudgetState)
	}

	// The reset time passes but the lifetime spend is unchanged: it must block,
	// once, instead of resuming and breaching again.
	g.f.clk.Advance(3 * time.Hour)
	g.c.checkBudget()
	g.noStarts()
	got := g.get(a)
	if got.Status != AutomationBlocked || got.BudgetState == nil || got.BudgetState.Action != BudgetActionBlocked {
		t.Fatalf("status=%s state=%+v, want blocked", got.Status, got.BudgetState)
	}
	if !strings.Contains(got.BudgetState.Reason, "still over") {
		t.Fatalf("reason = %q", got.BudgetState.Reason)
	}
	if g.alertCount("warn|Automation paused") != 1 || g.alertCount("warn|Automation blocked") != 1 || g.alertCount("") != 2 {
		t.Fatalf("alerts = %v, want one pause and one block alert", g.alerts)
	}
	if g.auditCount("automation_budget_resumed") != 0 {
		t.Fatalf("a refused resume was audited as resumed: %v", g.audits)
	}
	version := got.Version
	for i := 0; i < 3; i++ {
		g.f.clk.Advance(time.Hour)
		g.c.checkBudget()
	}
	if again := g.get(a); again.Status != AutomationBlocked || again.Version != version || g.alertCount("") != 2 {
		t.Fatalf("pause looped: status=%s version %d->%d alerts=%v", again.Status, version, again.Version, g.alerts)
	}
}

func TestBudgetBlockedNeedsOperatorResume(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	a := g.activateRunning(g.create(claudeLifetime(10), 2))
	g.overCeiling(12)
	g.finishFirst()
	blocked := g.get(a)
	if blocked.Status != AutomationBlocked {
		t.Fatalf("status = %s", blocked.Status)
	}

	// The tick never resumes it, whatever the clock says.
	g.f.clk.Advance(365 * 24 * time.Hour)
	g.c.checkBudget()
	if got := g.get(a); got.Status != AutomationBlocked {
		t.Fatalf("tick resumed a blocked Automation: %s", got.Status)
	}

	// The operator resume is refused with the reason while still over.
	_, err := g.m.ResumeAutomation(a.ID, "operator", blocked.Version)
	var refused *budgetRefusedError
	if !errors.As(err, &refused) || !strings.HasPrefix(err.Error(), "409") || !strings.Contains(err.Error(), "budget reached") {
		t.Fatalf("resume error = %v, want a 409 carrying the budget reason", err)
	}
	g.noStarts()
	if got := g.get(a); got.Status != AutomationBlocked || got.BudgetState == nil {
		t.Fatalf("a refused resume changed the Automation: %s %+v", got.Status, got.BudgetState)
	}

	// Once the operator raises the ceiling the resume goes through and starts
	// the held member.
	raised, err := g.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.BudgetPolicy.CeilingValue = 100
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.m.ResumeAutomation(a.ID, "operator", raised.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationActive || got.BudgetState != nil {
		t.Fatalf("after resume: status=%s state=%+v", got.Status, got.BudgetState)
	}
	runs, _ := g.fake.counts()
	if len(runs) != 1 || runs[0] != g.prds[1].ID {
		t.Fatalf("runs = %v, want the held member started", runs)
	}
	if rd := g.lastAudit("automation_budget_resumed"); rd == nil || rd["actor"] != "operator" || rd["action"] != BudgetActionBlocked {
		t.Fatalf("resumed audit = %v", rd)
	}
	if g.alertCount("info|Automation resumed") != 0 {
		t.Fatalf("an operator resume raised the automatic-resume alert: %v", g.alerts)
	}
}

func TestBudgetEstimateNeverBlocksAdvance(t *testing.T) {
	t.Run("allocation estimate", func(t *testing.T) {
		f := newBudgetEvalFixture(t)
		f.b.Allocation = func(llm string, spend float64) *BudgetAllocation {
			return &BudgetAllocation{AllocationPct: 4000, Estimated: true, PlanTier: "test"}
		}
		g := newGateHarness(t, 2, f)
		p := &BudgetPolicy{CeilingType: "allocation_pct", CeilingValue: 50, Window: "day", ResetZone: "UTC", Backend: "claude"}
		a := g.activateRunning(g.create(p, 2))
		g.overCeiling(5000)
		g.finishFirst()

		runs, _ := g.fake.counts()
		if len(runs) != 1 {
			t.Fatalf("runs = %v, want the next member started", runs)
		}
		got := g.get(a)
		if got.Status != AutomationActive || got.BudgetState != nil {
			t.Fatalf("an allocation estimate gated: status=%s state=%+v", got.Status, got.BudgetState)
		}
		if g.alertCount("") != 0 || g.auditCount("automation_budget_breach") != 0 {
			t.Fatalf("alerts=%v audits=%v", g.alerts, g.audits)
		}
	})

	t.Run("pacing hold", func(t *testing.T) {
		g := newGateHarness(t, 2, nil)
		p := claudeDay(10)
		p.Pacing = "even"
		a := g.activateRunning(g.create(p, 2))
		// Noon: half the day, so $5 is allowed so far and $6 is ahead of pace.
		g.f.entries = append(g.f.entries, fakeSpendEntry{at: g.f.clk.Now().Add(-time.Hour), member: "m0", llm: "claude", usd: 6})
		g.finishFirst()

		g.noStarts()
		got := g.get(a)
		if got.Status != AutomationActive || got.CurrentMemberID != "m1" || got.IdleSince == nil {
			t.Fatalf("status=%s cursor=%q idle=%v, want active on m1, idle", got.Status, got.CurrentMemberID, got.IdleSince)
		}
		if got.BudgetState == nil || got.BudgetState.Action != BudgetActionHeld {
			t.Fatalf("state = %+v, want held", got.BudgetState)
		}
		if g.alertCount("") != 0 || g.auditCount("automation_budget_breach") != 0 {
			t.Fatalf("a hold alerted or audited as a breach: %v %v", g.alerts, g.audits)
		}
		version := got.Version
		for i := 0; i < 3; i++ {
			g.c.checkBudget()
		}
		if again := g.get(a); again.Version != version || g.decisionCount(a, decisionBudgetHold) != 1 {
			t.Fatalf("hold repeated: version %d->%d, hold decisions %d", version, again.Version, g.decisionCount(a, decisionBudgetHold))
		}

		// Later in the day more is allowed and the tick starts the member.
		g.f.clk.Advance(6 * time.Hour)
		g.c.checkBudget()
		runs, _ := g.fake.counts()
		if len(runs) != 1 || runs[0] != g.prds[1].ID {
			t.Fatalf("runs = %v, want the held member started once the pace allows it", runs)
		}
		if after := g.get(a); after.Status != AutomationActive || after.BudgetState != nil || after.IdleSince != nil {
			t.Fatalf("after the hold cleared: status=%s state=%+v idle=%v", after.Status, after.BudgetState, after.IdleSince)
		}
	})

	t.Run("estimate hold", func(t *testing.T) {
		g := newGateHarness(t, 2, nil)
		members := []AutomationMember{
			{ID: "m0", PRDID: g.prds[0].ID, OnComplete: "m1"},
			{ID: "m1", PRDID: g.prds[1].ID, EstimatedCostUSD: 20},
		}
		a := g.activateRunning(g.createMembers(claudeDay(10), members, ""))
		g.finishFirst()

		g.noStarts()
		got := g.get(a)
		if got.Status != AutomationActive || got.BudgetState == nil || got.BudgetState.Action != BudgetActionHeld {
			t.Fatalf("status=%s state=%+v, want an active hold", got.Status, got.BudgetState)
		}
		if g.alertCount("") != 0 || g.auditCount("automation_budget_breach") != 0 {
			t.Fatalf("an estimate hold alerted or audited as a breach: %v %v", g.alerts, g.audits)
		}
	})
}

func TestBudgetNonOptedInPeerDoesNotBlockAdvance(t *testing.T) {
	// Local spend $6, opted-in peer "edge" $1, and a registered peer "other"
	// that is not opted in and reports $500: only edge counts.
	f, _, _, other := peerEvalFixture(t, 1, 500)
	f.entries[0].member = "m0" // spend by the finished member, not an estimate for m1
	g := newGateHarness(t, 2, f)
	p := claudeDay(10)
	p.PeerWindow = []string{"edge"}
	a := g.activateRunning(g.create(p, 2))
	g.finishFirst()

	runs, _ := g.fake.counts()
	if len(runs) != 1 || runs[0] != g.prds[1].ID {
		t.Fatalf("runs = %v, want the next member started", runs)
	}
	if got := g.get(a); got.Status != AutomationActive || got.BudgetState != nil {
		t.Fatalf("a peer that was not opted in gated: status=%s state=%+v", got.Status, got.BudgetState)
	}
	if other.calls.Load() != 0 {
		t.Fatalf("the peer that was not opted in was asked for usage %d time(s)", other.calls.Load())
	}

	// The opted-in peer does count: with edge at $5 the ceiling is reached.
	f2, _, _, _ := peerEvalFixture(t, 5, 500)
	f2.entries[0].member = "m0"
	g2 := newGateHarness(t, 2, f2)
	p2 := claudeDay(10)
	p2.PeerWindow = []string{"edge"}
	a2 := g2.activateRunning(g2.create(p2, 2))
	g2.finishFirst()
	g2.noStarts()
	got := g2.get(a2)
	if got.Status != AutomationPaused || got.BudgetState == nil || got.BudgetState.SpentUSD != 11 {
		t.Fatalf("status=%s state=%+v, want paused at $11 (local 6 + edge 5)", got.Status, got.BudgetState)
	}
}

func TestBudgetGateNoPolicyChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		evaluator bool
	}{{"evaluator wired", true}, {"no evaluator", false}} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGateHarness(t, 2, nil)
			if !tc.evaluator {
				g.m.SetAutomationBudget(nil)
			}
			a := g.activateRunning(g.create(nil, 2))
			g.overCeiling(100000)
			before := g.get(a)

			g.finishFirst()

			runs, _ := g.fake.counts()
			if len(runs) != 1 || runs[0] != g.prds[1].ID {
				t.Fatalf("runs = %v, want the next member started", runs)
			}
			got := g.get(a)
			if got.Status != AutomationActive || got.BudgetState != nil || got.IdleSince != nil || got.CurrentMemberID != "m1" {
				t.Fatalf("status=%s state=%+v idle=%v cursor=%q", got.Status, got.BudgetState, got.IdleSince, got.CurrentMemberID)
			}
			if got.Version != before.Version+1 {
				t.Fatalf("version %d, want %d (one write)", got.Version, before.Version+1)
			}
			if g.alertCount("") != 0 || g.auditCount("automation_budget_breach") != 0 {
				t.Fatalf("alerts=%v audits=%v", g.alerts, g.audits)
			}
			g.c.checkBudget() // a tick over an Automation with no episode does nothing
			if again := g.get(a); again.Version != got.Version {
				t.Fatalf("tick rewrote an Automation with no budget state")
			}
		})
	}
}
