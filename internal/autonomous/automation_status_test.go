package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func setIdle(t *testing.T, h *coordHarness, a *Automation, status AutomationStatus, since *time.Time) {
	t.Helper()
	_, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = status
		a.IdleSince = since
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAutomationStatusSummaryFields(t *testing.T) {
	h := newCoordHarness(t, 3)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}, {"m2", "", ""}}, 3))
	st := h.m.Status()
	if len(st.Automations) != 1 {
		t.Fatalf("automations=%d", len(st.Automations))
	}
	s := st.Automations[0]
	if s.ID != a.ID || s.Name != "chain" || s.Status != string(AutomationActive) {
		t.Fatalf("summary=%+v", s)
	}
	if s.CurrentMemberID != "m0" || s.CurrentPRDID != h.prds[0].ID || s.CurrentPRDTitle != "alpha" {
		t.Fatalf("cursor fields=%+v", s)
	}
	if s.PendingMembers != 2 || s.IdleSince != nil || s.IdleSeconds != 0 {
		t.Fatalf("pending/idle=%+v", s)
	}
	if s.SpentUSD != 0 || s.CeilingDisplay != "" {
		t.Fatalf("budget fields set: %+v", s)
	}
}

func TestAutomationStatusCountsPendingInserts(t *testing.T) {
	h := newCoordHarness(t, 3)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}, {"m2", "", ""}}, 3))
	_, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.PendingInserts = []PendingInsert{
			{ID: "ins1", PRDID: h.prds[1].ID, Actor: "operator", AddedAt: time.Now(), Status: MemberPending},
			{ID: "ins2", PRDID: h.prds[2].ID, Urgent: true, Actor: "session:x", AddedAt: time.Now(), Status: MemberPending},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := h.m.Status().Automations[0]
	// Two pending members plus two held inserts.
	if s.PendingMembers != 4 {
		t.Fatalf("pending_members=%d, want 2 members + 2 inserts", s.PendingMembers)
	}
}

func TestAutomationStatusPendingInsertsListed(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto(nil, 1))
	held := PendingInsert{ID: "ins1", PRDID: h.prds[0].ID, Name: "new step", Urgent: true, Actor: "operator", Reason: "operator asked", AddedAt: time.Now(), Status: MemberPending}
	_, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.PendingInserts = append(a.PendingInserts, held)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := h.m.Status().Automations[0]
	if len(s.PendingInserts) != 1 ||
		s.PendingInserts[0].ID != held.ID || s.PendingInserts[0].PRDID != held.PRDID ||
		s.PendingInserts[0].Name != held.Name || s.PendingInserts[0].Urgent != held.Urgent ||
		s.PendingInserts[0].Actor != held.Actor || s.PendingInserts[0].Reason != held.Reason ||
		!s.PendingInserts[0].AddedAt.Equal(held.AddedAt) || s.PendingInserts[0].Status != held.Status {
		t.Fatalf("pending_inserts=%+v, want %+v", s.PendingInserts, held)
	}
	// The summary carries a copy: mutating it must not touch the stored record.
	s.PendingInserts[0].Actor = "mutated"
	if stored, _ := h.m.GetAutomation(a.ID); len(stored.PendingInserts) != 1 || stored.PendingInserts[0].Actor != "operator" {
		t.Fatalf("stored record mutated through the summary: %+v", stored.PendingInserts)
	}
}

func TestAutomationStatusIdleSecondsAndBlockedReason(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	since := time.Now().Add(-90 * time.Second)
	_, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationBlocked
		a.IdleSince = &since
		a.Decisions = append(a.Decisions,
			Decision{At: since, Kind: "blocked", Note: "old reason"},
			Decision{At: since, Kind: "advance", Note: "ignored"},
			Decision{At: since, Kind: "blocked", Note: "newest reason"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := h.m.Status().Automations[0]
	if s.IdleSeconds < 90 || s.IdleSeconds > 95 {
		t.Fatalf("idle seconds=%d", s.IdleSeconds)
	}
	if s.BlockedReason != "newest reason" {
		t.Fatalf("blocked reason=%q", s.BlockedReason)
	}
}

func TestAutomationStatusOmitsTerminal(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto(nil, 1))
	b, err := h.m.CreateAutomation(AutomationInput{Name: "second", Members: []AutomationMember{{ID: "x0", PRDID: h.prds[1].ID}}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	setIdle(t, h, a, AutomationCompleted, nil)
	setIdle(t, h, b, AutomationCancelled, nil)
	if got := h.m.Status().Automations; len(got) != 0 {
		t.Fatalf("terminal automations listed: %+v", got)
	}
}

// idleHarness returns a harness whose coordinator clock is controllable and
// whose threshold is minutes.
func idleHarness(t *testing.T, minutes int) (*coordHarness, *time.Time) {
	t.Helper()
	h := newCoordHarness(t, 1)
	cfg := h.m.Config()
	cfg.Automation.IdleAlertMinutes = minutes
	h.m.SetConfig(cfg)
	now := time.Now()
	h.c.now = func() time.Time { return now }
	return h, &now
}

func TestAutomationIdleOneAlertPerEpisode(t *testing.T) {
	h, now := idleHarness(t, 10)
	a := h.force(h.makeAuto(nil, 1))
	since := *now
	setIdle(t, h, a, AutomationActive, &since)

	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 0 {
		t.Fatalf("alert before threshold: %d", n)
	}
	*now = since.Add(11 * time.Minute)
	h.c.checkIdle()
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 1 {
		t.Fatalf("alerts in one episode=%d", n)
	}

	// Advancing clears the episode; a later idle period alerts again.
	setIdle(t, h, a, AutomationActive, nil)
	h.c.checkIdle()
	since2 := *now
	setIdle(t, h, a, AutomationActive, &since2)
	*now = since2.Add(11 * time.Minute)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 2 {
		t.Fatalf("alerts after second episode=%d", n)
	}
}

func TestAutomationIdleNoAlertWhenOff(t *testing.T) {
	h, now := idleHarness(t, 0)
	a := h.force(h.makeAuto(nil, 1))
	since := now.Add(-24 * time.Hour)
	setIdle(t, h, a, AutomationActive, &since)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 0 {
		t.Fatalf("alerts with threshold 0: %d", n)
	}
}

func TestAutomationIdleNoAlertWhenPaused(t *testing.T) {
	h, now := idleHarness(t, 5)
	a := h.force(h.makeAuto(nil, 1))
	since := now.Add(-time.Hour)
	setIdle(t, h, a, AutomationPaused, &since)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 0 {
		t.Fatalf("alerts while paused: %d", n)
	}
}

func TestAutomationIdleThresholdReadLive(t *testing.T) {
	h, now := idleHarness(t, 0)
	a := h.force(h.makeAuto(nil, 1))
	since := now.Add(-time.Hour)
	setIdle(t, h, a, AutomationActive, &since)
	h.c.checkIdle()
	cfg := h.m.Config()
	cfg.Automation.IdleAlertMinutes = 30
	h.m.SetConfig(cfg)
	h.c.checkIdle()
	if n := h.alertCount("warn"); n != 1 {
		t.Fatalf("alerts after live threshold change: %d", n)
	}
}

func TestAutomationIdleStartStop(t *testing.T) {
	h := newCoordHarness(t, 1)
	h.c.idleTick = time.Millisecond
	h.c.Start()
	h.c.Start() // no-op
	time.Sleep(5 * time.Millisecond)
	h.c.Stop()
	h.c.Stop()
	h.c.Start()
	h.c.Stop()
}

// budgetSummary returns the status row of a, read through the real status path.
func budgetSummary(t *testing.T, g *gateHarness, a *Automation) AutomationStatusSummary {
	t.Helper()
	for _, s := range g.m.Status().Automations {
		if s.ID == a.ID {
			return s
		}
	}
	t.Fatalf("automation %s missing from status", a.ID)
	return AutomationStatusSummary{}
}

func TestAutomationStatusSummaryCarriesBudget(t *testing.T) {
	g := newGateHarness(t, 3, nil)
	// The fake clock reads Thursday 2026-10-22 12:00 UTC; the week runs from Monday.
	p := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 40, Window: "week", Backend: "claude", ResetZone: "UTC"}
	a := g.activateRunning(g.create(p, 2))
	g.f.entries = append(g.f.entries, fakeSpendEntry{at: g.f.clk.Now().Add(-time.Hour), member: "m0", llm: "claude", usd: 12.40})

	s := budgetSummary(t, g, a)
	if s.SpentUSD != 12.40 || s.CeilingDisplay != "$12.40 of $40.00 (week)" {
		t.Fatalf("spent %v display %q", s.SpentUSD, s.CeilingDisplay)
	}
	b := s.Budget
	if b == nil || b.Scope != "automation" || b.CeilingUSD != 40 || b.SpentUSD != 12.40 || b.RemainingUSD != 27.60 || b.Window != "week" {
		t.Fatalf("budget view %+v", b)
	}
	if b.WindowStart == nil || !b.WindowStart.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) ||
		b.WindowEnd == nil || !b.WindowEnd.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window %v..%v", b.WindowStart, b.WindowEnd)
	}
	if b.ResetAt == nil || !b.ResetAt.Equal(*b.WindowEnd) || !b.ResetKnown || b.ResetSource != BudgetResetWindow {
		t.Fatalf("reset %v %v %q", b.ResetAt, b.ResetKnown, b.ResetSource)
	}
	if b.CeilingReached || b.Hold != "" || b.State != nil || b.Allocation != nil || b.Pacing != "none" {
		t.Fatalf("budget view %+v", b)
	}

	// The summary reaches the wire under the names the REST status carries.
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["ceiling_display"] != "$12.40 of $40.00 (week)" || wire["spent_usd"] != 12.40 {
		t.Fatalf("wire %v", wire)
	}
	if bw, _ := wire["budget"].(map[string]any); bw == nil || bw["ceiling_usd"] != 40.0 || bw["remaining_usd"] != 27.60 {
		t.Fatalf("wire budget %v", wire["budget"])
	}

	// A stored breach episode rides along, and a member override reports its own scope.
	g.forceState(a, func(a *Automation) {
		a.BudgetState = &BudgetState{Action: BudgetActionHeld, Scope: "automation", Reason: "ahead of pace"}
	})
	if st := budgetSummary(t, g, a).Budget.State; st == nil || st.Action != BudgetActionHeld {
		t.Fatalf("stored state not in the view: %+v", st)
	}
	over := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 20, Window: "day", Backend: "claude", ResetZone: "UTC"}
	g.forceState(a, func(a *Automation) { automationMember(a, "m0").BudgetPolicy = over })
	s = budgetSummary(t, g, a)
	if s.Budget.Scope != "member:m0" || s.CeilingDisplay != "$12.40 of $20.00 (day, member m0)" {
		t.Fatalf("member scope %q display %q", s.Budget.Scope, s.CeilingDisplay)
	}

	// An Automation with no policy has no budget fields at all.
	plain := g.m.automations
	other, err := g.m.CreateAutomation(AutomationInput{Name: "plain", Members: []AutomationMember{{ID: "x0", PRDID: g.prds[2].ID}}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Update(other.ID, 0, func(a *Automation) error { a.Status = AutomationActive; return nil }); err != nil {
		t.Fatal(err)
	}
	if s := budgetSummary(t, g, other); s.Budget != nil || s.SpentUSD != 0 || s.CeilingDisplay != "" {
		t.Fatalf("plain automation row %+v", s)
	}

	// The stored record never holds the computed view; the read view does.
	stored, _ := g.m.GetAutomation(a.ID)
	if raw, _ := json.Marshal(stored); strings.Contains(string(raw), `"budget":`) {
		t.Fatalf("stored record carries the computed view: %s", raw)
	}
	raw, _ = json.Marshal(g.m.AutomationViewOf(stored))
	var view struct {
		ID     string      `json:"id"`
		Budget *BudgetView `json:"budget"`
	}
	if err := json.Unmarshal(raw, &view); err != nil || view.ID != a.ID || view.Budget == nil || view.Budget.Scope != "member:m0" {
		t.Fatalf("automation view %s (%v)", raw, err)
	}
	if v := g.m.AutomationViewOf(stored); v.Automation != stored {
		t.Fatal("the view must wrap the stored record")
	}
}

func TestAutomationStatusBudgetEstimateLabelled(t *testing.T) {
	g := newGateHarness(t, 1, nil)
	// A notional $5 allocation against $8 of spend reads 160%, well over a
	// hundred, while the $10 dollar ceiling still has room.
	g.f.b.Allocation = func(llm string, spend float64) *BudgetAllocation {
		return &BudgetAllocation{AllocationPct: spend / 5 * 100, Estimated: true, AllocationUSD: 5, PlanTier: "pro", Basis: allocationBasis}
	}
	p := claudeDay(10)
	p.AllocationEstimate = true
	a := g.activateRunning(g.create(p, 1))
	g.overCeiling(8)

	s := budgetSummary(t, g, a)
	b := s.Budget
	if b == nil || b.Allocation == nil || !b.Allocation.Estimated || b.Allocation.Basis == "" || b.Allocation.AllocationPct != 160 {
		t.Fatalf("allocation estimate %+v", b)
	}
	if b.CeilingReached || b.Hold != "" || b.RemainingUSD != 2 {
		t.Fatalf("the estimate gated: %+v", b)
	}
	if v := g.f.b.Evaluate(g.get(a), nil, false); v.Gate {
		t.Fatalf("verdict gates on the estimate: %+v", v)
	}
	// The figure lives only inside the estimate object.
	raw, _ := json.Marshal(s)
	var wire map[string]any
	_ = json.Unmarshal(raw, &wire)
	if _, top := wire["allocation_pct"]; top {
		t.Fatalf("allocation figure at the top of the row: %s", raw)
	}
	est, _ := wire["budget"].(map[string]any)["allocation_estimate"].(map[string]any)
	if est == nil || est["estimated"] != true || est["allocation_pct"] != 160.0 || est["basis"] == "" {
		t.Fatalf("estimate object %v", est)
	}
	if strings.Contains(s.CeilingDisplay, "%") {
		t.Fatalf("display %q carries the estimate", s.CeilingDisplay)
	}
	if got := g.get(a); got.Status != AutomationActive || got.BudgetState != nil {
		t.Fatalf("automation moved: %s %+v", got.Status, got.BudgetState)
	}

	// Off by default: the same policy without the opt-in reports no estimate.
	q := claudeDay(10)
	b2 := g.m.BudgetViewFor(&Automation{ID: "x", Status: AutomationActive, BudgetPolicy: q, Members: []AutomationMember{{ID: "m0", PRDID: g.prds[0].ID}}})
	if b2 == nil || b2.Allocation != nil {
		t.Fatalf("estimate without opt-in: %+v", b2)
	}

	// An allocation_pct ceiling is an estimate only and never gates, however much is spent.
	g.overCeiling(1000)
	est100 := &BudgetPolicy{CeilingType: "allocation_pct", CeilingValue: 50, Window: "day", Backend: "claude", ResetZone: "UTC"}
	b3 := g.m.BudgetViewFor(&Automation{ID: "y", Status: AutomationActive, BudgetPolicy: est100, Members: []AutomationMember{{ID: "m0", PRDID: g.prds[0].ID}}})
	if b3 == nil || b3.CeilingReached || b3.Hold != "" || b3.Allocation == nil || !b3.Allocation.Estimated || b3.Note == "" {
		t.Fatalf("allocation_pct view %+v", b3)
	}
	if v := g.f.b.Evaluate(&Automation{ID: "y", BudgetPolicy: est100}, nil, false); v.Gate {
		t.Fatalf("allocation_pct policy gated: %+v", v)
	}
}

func TestAutomationStatusBudgetPeersNoAutoConfirmation(t *testing.T) {
	f, pa, _, _ := peerEvalFixture(t, 2, 0)
	g := newGateHarness(t, 1, f)
	a := g.activateRunning(g.create(pa.BudgetPolicy, 1)) // the fixture holds $6 of local spend

	b := budgetSummary(t, g, a).Budget
	if b == nil || b.PeerSpendUSD != 2 || b.SpentUSD != 8 {
		t.Fatalf("budget view %+v, want $6 local plus $2 from the opted-in peer", b)
	}
	if !strings.Contains(b.PeerBasis, "no automatic confirmation") {
		t.Fatalf("peer basis %q", b.PeerBasis)
	}
	var edge, other *PeerBudgetStatus
	for i := range b.Peers {
		switch b.Peers[i].Peer {
		case "edge":
			edge = &b.Peers[i]
		case "other":
			other = &b.Peers[i]
		}
	}
	if edge == nil || !edge.Counted || edge.SpendUSD != 2 || !strings.Contains(edge.Note, "no automatic confirmation") || !strings.Contains(edge.Note, "opted this peer in") {
		t.Fatalf("opted-in line %+v", edge)
	}
	if other == nil || other.Counted || other.SpendUSD != 0 || other.Reason == "" {
		t.Fatalf("peer that is not opted in %+v", other)
	}
	if b.CeilingReached {
		t.Fatalf("view %+v", b)
	}
}
