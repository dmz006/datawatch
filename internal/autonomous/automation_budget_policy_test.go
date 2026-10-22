package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func dollarPolicy(window string) *BudgetPolicy {
	return &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: window, Pacing: "none", OnBreach: "pause"}
}

func TestBudgetPolicyValidateNewFields(t *testing.T) {
	p := dollarPolicy("week")
	p.AllocationEstimate = true
	p.PeerWindow = []string{" alpha ", "beta"}
	p.ResetTime = "06:30"
	p.ResetWeekday = "Friday"
	p.ResetZone = "America/New_York"
	if err := validateBudgetPolicy(p); err != nil {
		t.Fatalf("valid week policy rejected: %v", err)
	}
	if p.PeerWindow[0] != "alpha" {
		t.Errorf("peer_window entry not trimmed: %q", p.PeerWindow[0])
	}

	m := dollarPolicy("month")
	m.ResetDay = 28
	m.ResetTime = "00:00"
	if err := validateBudgetPolicy(m); err != nil {
		t.Fatalf("valid month policy rejected: %v", err)
	}
	d := dollarPolicy("day")
	d.ResetTime = "23:59"
	d.ResetZone = "UTC"
	if err := validateBudgetPolicy(d); err != nil {
		t.Fatalf("valid day policy rejected: %v", err)
	}
	if err := validateBudgetPolicy(dollarPolicy("automation")); err != nil {
		t.Fatalf("lifetime policy without anchors rejected: %v", err)
	}

	var nilPolicy *BudgetPolicy
	if got := nilPolicy.EffectiveOnBreach(); got != "pause" {
		t.Errorf("nil EffectiveOnBreach = %q, want pause", got)
	}
	if got := (&BudgetPolicy{}).EffectiveOnBreach(); got != "pause" {
		t.Errorf("empty EffectiveOnBreach = %q, want pause", got)
	}
	if got := (&BudgetPolicy{OnBreach: "block"}).EffectiveOnBreach(); got != "block" {
		t.Errorf("EffectiveOnBreach = %q, want block", got)
	}
	if got := (&BudgetPolicy{}).EffectivePacing(); got != "none" {
		t.Errorf("empty EffectivePacing = %q, want none", got)
	}
	if got := (&BudgetPolicy{Pacing: "even"}).EffectivePacing(); got != "even" {
		t.Errorf("EffectivePacing = %q, want even", got)
	}
}

func TestBudgetPolicyResetAnchorValidation(t *testing.T) {
	cases := []struct {
		name   string
		window string
		mutate func(*BudgetPolicy)
		want   string
	}{
		{"bad reset_time", "day", func(p *BudgetPolicy) { p.ResetTime = "25:00" }, "reset_time"},
		{"reset_time not HH:MM", "day", func(p *BudgetPolicy) { p.ResetTime = "6pm" }, "reset_time"},
		{"bad weekday", "week", func(p *BudgetPolicy) { p.ResetWeekday = "someday" }, "reset_weekday"},
		{"reset_day zero is unset but 29 is high", "month", func(p *BudgetPolicy) { p.ResetDay = 29 }, "reset_day"},
		{"reset_day negative", "month", func(p *BudgetPolicy) { p.ResetDay = -1 }, "reset_day"},
		{"bad zone", "day", func(p *BudgetPolicy) { p.ResetZone = "Mars/Olympus" }, "reset_zone"},
		{"weekday on day window", "day", func(p *BudgetPolicy) { p.ResetWeekday = "monday" }, "reset_weekday"},
		{"weekday on month window", "month", func(p *BudgetPolicy) { p.ResetWeekday = "monday" }, "reset_weekday"},
		{"day on week window", "week", func(p *BudgetPolicy) { p.ResetDay = 3 }, "reset_day"},
		{"day on day window", "day", func(p *BudgetPolicy) { p.ResetDay = 3 }, "reset_day"},
		{"time on lifetime window", "automation", func(p *BudgetPolicy) { p.ResetTime = "06:00" }, "reset_time"},
		{"zone on lifetime window", "automation", func(p *BudgetPolicy) { p.ResetZone = "UTC" }, "reset_zone"},
		{"weekday on lifetime window", "automation", func(p *BudgetPolicy) { p.ResetWeekday = "monday" }, "reset_weekday"},
		{"empty peer", "day", func(p *BudgetPolicy) { p.PeerWindow = []string{"a", "  "} }, "peer_window"},
		{"duplicate peer", "day", func(p *BudgetPolicy) { p.PeerWindow = []string{"a", " a"} }, "peer_window"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := dollarPolicy(tc.window)
			tc.mutate(p)
			err := validateBudgetPolicy(p)
			if err == nil {
				t.Fatal("want a validation error, got nil")
			}
			if !strings.HasPrefix(err.Error(), "automation invalid: ") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q: want prefix %q and mention %q", err, "automation invalid: ", tc.want)
			}
		})
	}
}

func TestBudgetPolicyOldJSONStillLoads(t *testing.T) {
	old := `{"ceiling_type":"dollar","ceiling_value":12.5,"window":"week","pacing":"even","on_breach":"block","backend":"claude-code"}`
	var p BudgetPolicy
	if err := json.Unmarshal([]byte(old), &p); err != nil {
		t.Fatal(err)
	}
	if err := validateBudgetPolicy(&p); err != nil {
		t.Fatalf("old record fails validation: %v", err)
	}
	if p.AllocationEstimate || len(p.PeerWindow) != 0 || p.ResetTime != "" || p.ResetWeekday != "" || p.ResetDay != 0 || p.ResetZone != "" {
		t.Errorf("new fields not zero on an old record: %+v", p)
	}
	out, err := json.Marshal(&p)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal([]byte(old), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("round trip changed the record:\n before %v\n after  %v", before, after)
	}

	// An old Automation record carries no budget_state and members carry no
	// estimated cost; neither is written back.
	a := Automation{ID: "a1b2c3d4", Name: "x", Status: AutomationDraft, Members: []AutomationMember{{ID: "m1", PRDID: "p1"}}, BudgetPolicy: &p}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "budget_state") || strings.Contains(string(raw), "estimated_cost_usd") {
		t.Errorf("unset runtime fields were serialised: %s", raw)
	}
}

func TestBudgetPolicyAllocationTypeIsEstimateOnly(t *testing.T) {
	p := &BudgetPolicy{CeilingType: "allocation_pct", CeilingValue: 50, Window: "week"}
	if err := validateBudgetPolicy(p); err != nil {
		t.Fatalf("allocation_pct must stay valid: %v", err)
	}
	if p.Gates() {
		t.Error("allocation_pct policy reports as gating; it is estimate-only")
	}
	if !dollarPolicy("day").Gates() {
		t.Error("dollar policy should gate")
	}
	var nilPolicy *BudgetPolicy
	if nilPolicy.Gates() {
		t.Error("nil policy should not gate")
	}
}

func TestAutomationBudgetStateRoundTrip(t *testing.T) {
	resume := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	st := &BudgetState{
		Action:      BudgetActionPaused,
		BreachedAt:  time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC),
		Reason:      "weekly ceiling reached",
		Scope:       "member:abcd1234",
		SpentUSD:    10.5,
		CeilingUSD:  10,
		Window:      "week",
		ResumeAt:    &resume,
		ResetKnown:  true,
		ResetSource: BudgetResetWindow,
	}
	a := &Automation{ID: "a1b2c3d4", Name: "n", Status: AutomationPaused, BudgetState: st}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var back Automation
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.BudgetState, st) {
		t.Errorf("budget_state round trip:\n got  %+v\n want %+v", back.BudgetState, st)
	}
	// The store clone keeps it too.
	if c := cloneAutomation(a); !reflect.DeepEqual(c.BudgetState, st) {
		t.Errorf("clone lost budget_state: %+v", c.BudgetState)
	}

	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := makeAutomationMemberPRDs(t, m, 1)[0]

	// A caller-supplied state on the wire is dropped by create.
	var in AutomationInput
	body := `{"name":"b","budget_state":{"action":"blocked","scope":"automation","breached_at":"2026-10-22T09:30:00Z"}}`
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatal(err)
	}
	in.Members = []AutomationMember{{PRDID: p.ID}}
	created, err := m.CreateAutomation(in, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if created.BudgetState != nil {
		t.Fatalf("create kept a caller-supplied budget_state: %+v", created.BudgetState)
	}

	// The coordinator writes it through the store; update must preserve it
	// and ignore one sent by the caller.
	stored, err := m.automations.Update(created.ID, 0, func(x *Automation) error {
		x.BudgetState = st
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	in.Name = "renamed"
	upd, err := m.UpdateAutomation(created.ID, stored.Version, in, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(upd.BudgetState, st) {
		t.Errorf("update changed the stored budget_state: %+v", upd.BudgetState)
	}
	got, _ := m.GetAutomation(created.ID)
	if got == nil || !reflect.DeepEqual(got.BudgetState, st) {
		t.Errorf("persisted budget_state differs: %+v", got)
	}
}

func TestMemberEstimatedCostValidation(t *testing.T) {
	prdMember := func(cost float64) *Automation {
		return &Automation{
			ID: "a1b2c3d4", Name: "n", Status: AutomationDraft, EntryMemberID: "m1",
			Members: []AutomationMember{{ID: "m1", PRDID: "p1", EstimatedCostUSD: cost, Status: MemberPending}},
		}
	}
	if err := validateAutomation(prdMember(0)); err != nil {
		t.Fatalf("zero estimate rejected: %v", err)
	}
	if err := validateAutomation(prdMember(3.25)); err != nil {
		t.Fatalf("positive estimate on a prd member rejected: %v", err)
	}
	err := validateAutomation(prdMember(-1))
	if err == nil || !strings.Contains(err.Error(), "estimated_cost_usd") {
		t.Errorf("negative estimate: want estimated_cost_usd error, got %v", err)
	}

	// Rejected on every other kind.
	a := prdMember(0)
	a.Members = []AutomationMember{
		{ID: "m1", PRDID: "p1", Status: MemberPending},
		{ID: "m2", Kind: MemberKindAutomation, AutomationID: "other", EstimatedCostUSD: 2, Status: MemberPending},
	}
	if err := validateAutomation(a); err == nil || !strings.Contains(err.Error(), "estimated_cost_usd") {
		t.Errorf("estimate on an automation member: got %v", err)
	}
	for _, kind := range []string{MemberKindFork, MemberKindJoin} {
		mem := AutomationMember{ID: "m2", Kind: kind, EstimatedCostUSD: 1}
		if err := validateMemberKind(nil, mem); err == nil || !strings.Contains(err.Error(), "estimated_cost_usd") {
			t.Errorf("estimate on kind %s: got %v", kind, err)
		}
	}
}
