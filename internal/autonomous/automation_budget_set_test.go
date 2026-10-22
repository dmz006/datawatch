package autonomous

import (
	"strings"
	"testing"
)

// weekRequest is a complete Automation-level request: $40 a week on claude,
// paced evenly, pausing on breach.
func weekRequest() BudgetSetRequest {
	return BudgetSetRequest{
		CeilingType: "dollar", CeilingValue: 40, Window: "week", Pacing: "even", OnBreach: "pause",
		Backend: "claude", ResetWeekday: "tuesday", ResetTime: "09:30", ResetZone: "UTC",
	}
}

// twoMemberAutomation is an active m0 -> m1 chain with m0 running.
func twoMemberAutomation(t *testing.T) (*coordHarness, *Automation) {
	t.Helper()
	h, _ := newControlHarness(t, 2)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	return h, a
}

func newestDecision(a *Automation) Decision { return a.Decisions[len(a.Decisions)-1] }

func TestManagerSetBudgetPolicy(t *testing.T) {
	t.Run("sets the Automation policy and leaves the status alone", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		req := weekRequest()
		req.Reason = "weekly cap"
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, req, "alice", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != AutomationActive || got.Version != a.Version+1 {
			t.Fatalf("status %s version %d", got.Status, got.Version)
		}
		p := got.BudgetPolicy
		if p == nil || p.CeilingType != "dollar" || p.CeilingValue != 40 || p.Window != "week" || p.Pacing != "even" ||
			p.OnBreach != "pause" || p.Backend != "claude" || p.ResetWeekday != "tuesday" || p.ResetTime != "09:30" || p.ResetZone != "UTC" {
			t.Fatalf("policy %+v", p)
		}
		if stored, _ := h.m.GetAutomation(a.ID); stored.BudgetPolicy == nil || stored.BudgetPolicy.CeilingValue != 40 {
			t.Fatalf("policy was not stored: %+v", stored.BudgetPolicy)
		}
		d := newestDecision(got)
		if d.Kind != "budget_policy" || d.Actor != "alice" {
			t.Fatalf("decision %+v", d)
		}
		det := h.auditDetail("automation_set_budget")
		if det == nil || det["actor"] != "alice" || det["reason"] != "weekly cap" || det["automation_id"] != a.ID ||
			det["member_id"] != "" || det["ceiling_value"] != 40.0 || det["version"] != got.Version {
			t.Fatalf("audit %v", det)
		}
	})

	t.Run("replaces an existing policy and defaults the actor", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		first, err := h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		if newestDecision(first).Actor != "operator" {
			t.Fatalf("actor %q", newestDecision(first).Actor)
		}
		second, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{CeilingType: "dollar", CeilingValue: 5, Window: "day"}, "", first.Version)
		if err != nil {
			t.Fatal(err)
		}
		if p := second.BudgetPolicy; p.CeilingValue != 5 || p.Window != "day" || p.Backend != "" || p.Pacing != "" || p.ResetWeekday != "" {
			t.Fatalf("the new policy must replace the old one entirely: %+v", p)
		}
	})

	t.Run("does not move a paused Automation", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		p, err := h.m.PauseAutomation(a.ID, "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", p.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != AutomationPaused {
			t.Fatalf("status %s, want paused", got.Status)
		}
	})

	t.Run("a draft may be given a policy", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		d, err := h.m.CreateAutomation(AutomationInput{Name: "draft", EntryMemberID: "m0", Members: []AutomationMember{h.cm(0, "", "", "")}}, "operator")
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.m.SetAutomationBudgetPolicy(d.ID, weekRequest(), "", d.Version)
		if err != nil || got.Status != AutomationDraft || got.BudgetPolicy == nil {
			t.Fatalf("draft: %+v, %v", got, err)
		}
	})

	t.Run("refuses invalid input without writing", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		for name, req := range map[string]BudgetSetRequest{
			"empty":              {},
			"zero ceiling":       {CeilingType: "dollar", Window: "day"},
			"unknown ceiling":    {CeilingType: "euros", CeilingValue: 1, Window: "day"},
			"unknown window":     {CeilingType: "dollar", CeilingValue: 1, Window: "fortnight"},
			"unknown pacing":     {CeilingType: "dollar", CeilingValue: 1, Window: "day", Pacing: "fast"},
			"unknown on_breach":  {CeilingType: "dollar", CeilingValue: 1, Window: "day", OnBreach: "explode"},
			"bad reset time":     {CeilingType: "dollar", CeilingValue: 1, Window: "day", ResetTime: "25:99"},
			"weekday on a day":   {CeilingType: "dollar", CeilingValue: 1, Window: "day", ResetWeekday: "monday"},
			"reset day on week":  {CeilingType: "dollar", CeilingValue: 1, Window: "week", ResetDay: 3},
			"unknown zone":       {CeilingType: "dollar", CeilingValue: 1, Window: "day", ResetZone: "Mars/Olympus"},
			"blank peer":         {CeilingType: "dollar", CeilingValue: 1, Window: "day", PeerWindow: []string{" "}},
			"estimate on policy": {CeilingType: "dollar", CeilingValue: 1, Window: "day", EstimatedCostUSD: ptr(2.0)},
			"clear with fields":  {Clear: true, CeilingValue: 1},
		} {
			_, err := h.m.SetAutomationBudgetPolicy(a.ID, req, "", a.Version)
			if err == nil {
				t.Errorf("%s: want an error", name)
				continue
			}
			if strings.HasPrefix(err.Error(), "409: ") || strings.HasPrefix(err.Error(), "404: ") {
				t.Errorf("%s: invalid input must be a plain (400) error, got %q", name, err)
			}
			if strings.Contains(err.Error(), "BL") {
				t.Errorf("%s: error carries a backlog identifier: %q", name, err)
			}
		}
		if cur, _ := h.m.GetAutomation(a.ID); cur.Version != a.Version || cur.BudgetPolicy != nil {
			t.Fatalf("a refused request wrote: version %d policy %+v", cur.Version, cur.BudgetPolicy)
		}
		if h.auditDetail("automation_set_budget") != nil {
			t.Fatal("a refused request was audited")
		}
	})

	t.Run("refuses a finished Automation and an unknown one", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		h.forceState(a, func(a *Automation) { a.Status = AutomationCompleted })
		cur, _ := h.m.GetAutomation(a.ID)
		_, err := h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", cur.Version)
		wantErr(t, err, "409: ", "completed")
		cancelled, _ := h.m.automations.Update(a.ID, 0, func(a *Automation) error { a.Status = AutomationCancelled; return nil })
		_, err = h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", cancelled.Version)
		wantErr(t, err, "409: ", "cancelled")
		_, err = h.m.SetAutomationBudgetPolicy("nope", weekRequest(), "", 1)
		wantErr(t, err, "404: ")
	})
}

func TestManagerSetBudgetMemberOverride(t *testing.T) {
	t.Run("sets one member's override only", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		whole, err := h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		req := BudgetSetRequest{MemberID: "m1", CeilingType: "dollar", CeilingValue: 3, Window: "day", OnBreach: "block", Backend: "claude"}
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, req, "alice", whole.Version)
		if err != nil {
			t.Fatal(err)
		}
		if p := automationMember(got, "m1").BudgetPolicy; p == nil || p.CeilingValue != 3 || p.OnBreach != "block" {
			t.Fatalf("member policy %+v", p)
		}
		if automationMember(got, "m0").BudgetPolicy != nil {
			t.Fatal("the other member got an override")
		}
		if got.BudgetPolicy == nil || got.BudgetPolicy.CeilingValue != 40 {
			t.Fatalf("the Automation policy changed: %+v", got.BudgetPolicy)
		}
		if got.Status != AutomationActive {
			t.Fatalf("status %s", got.Status)
		}
		det := h.auditDetail("automation_set_budget")
		if det == nil || det["member_id"] != "m1" || det["actor"] != "alice" {
			t.Fatalf("audit %v", det)
		}
		if d := newestDecision(got); d.Kind != "budget_policy" || !strings.Contains(d.Note, "m1") {
			t.Fatalf("decision %+v", d)
		}
	})

	t.Run("a running member still takes an override", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m0", CeilingType: "dollar", CeilingValue: 2, Window: "automation"}, "", a.Version)
		if err != nil || automationMember(got, "m0").BudgetPolicy == nil {
			t.Fatalf("running member: %v", err)
		}
	})

	t.Run("sets and removes the declared cost without touching the policy", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		withPolicy, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m1", CeilingType: "dollar", CeilingValue: 9, Window: "day"}, "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		est, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m1", EstimatedCostUSD: ptr(4.5)}, "", withPolicy.Version)
		if err != nil {
			t.Fatal(err)
		}
		mem := automationMember(est, "m1")
		if mem.EstimatedCostUSD != 4.5 || mem.BudgetPolicy == nil || mem.BudgetPolicy.CeilingValue != 9 {
			t.Fatalf("member %+v", mem)
		}
		if det := h.auditDetail("automation_set_budget"); det["estimated_cost_usd"] != 4.5 {
			t.Fatalf("audit %v", det)
		}
		off, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m1", EstimatedCostUSD: ptr(0.0)}, "", est.Version)
		if err != nil || automationMember(off, "m1").EstimatedCostUSD != 0 {
			t.Fatalf("estimate not removed: %v", err)
		}
	})

	t.Run("refuses a member that has finished, is unknown or is a fork", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		h.forceState(a, func(a *Automation) {
			automationMember(a, "m0").Status = MemberCompleted
		})
		cur, _ := h.m.GetAutomation(a.ID)
		req := BudgetSetRequest{MemberID: "m0", CeilingType: "dollar", CeilingValue: 1, Window: "day"}
		_, err := h.m.SetAutomationBudgetPolicy(a.ID, req, "", cur.Version)
		wantErr(t, err, "409: ", "m0", "completed")
		req.MemberID = "ghost"
		_, err = h.m.SetAutomationBudgetPolicy(a.ID, req, "", cur.Version)
		wantErr(t, err, "404: ", "ghost")
		if after, _ := h.m.GetAutomation(a.ID); after.Version != cur.Version {
			t.Fatal("a refused request wrote")
		}

		fh, _ := newControlHarness(t, 4)
		fork, err := fh.m.CreateAutomation(AutomationInput{Name: "fj", EntryMemberID: "e0", Members: fh.forkMembers("", "")}, "operator")
		if err != nil {
			t.Fatal(err)
		}
		_, err = fh.m.SetAutomationBudgetPolicy(fork.ID, BudgetSetRequest{MemberID: "f1", CeilingType: "dollar", CeilingValue: 1, Window: "day"}, "", fork.Version)
		wantErr(t, err, "fork")
	})

	t.Run("a declared cost needs a member", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		_, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{EstimatedCostUSD: ptr(1.0)}, "", a.Version)
		wantErr(t, err, "member_id")
		_, err = h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m1", EstimatedCostUSD: ptr(-1.0)}, "", a.Version)
		wantErr(t, err, "negative")
	})
}

func TestManagerSetBudgetClear(t *testing.T) {
	t.Run("removes the Automation policy", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		set, err := h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{Clear: true, Reason: "no longer needed"}, "alice", set.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.BudgetPolicy != nil || got.Status != AutomationActive {
			t.Fatalf("policy %+v status %s", got.BudgetPolicy, got.Status)
		}
		if d := newestDecision(got); d.Kind != "budget_policy" || !strings.Contains(d.Note, "cleared") {
			t.Fatalf("decision %+v", d)
		}
		det := h.auditDetail("automation_set_budget")
		if det == nil || det["clear"] != true || det["reason"] != "no longer needed" {
			t.Fatalf("audit %v", det)
		}
	})

	t.Run("removes a member override and keeps the Automation policy", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		whole, _ := h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", a.Version)
		over, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m1", CeilingType: "dollar", CeilingValue: 3, Window: "day"}, "", whole.Version)
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m1", Clear: true}, "", over.Version)
		if err != nil {
			t.Fatal(err)
		}
		if automationMember(got, "m1").BudgetPolicy != nil {
			t.Fatal("override still set")
		}
		if got.BudgetPolicy == nil || got.BudgetPolicy.CeilingValue != 40 {
			t.Fatalf("the Automation policy changed: %+v", got.BudgetPolicy)
		}
	})

	t.Run("clearing nothing is not an error", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{Clear: true}, "", a.Version)
		if err != nil || got.BudgetPolicy != nil {
			t.Fatalf("clear of an unset policy: %+v, %v", got, err)
		}
	})

	t.Run("does not touch the status or an open breach episode", func(t *testing.T) {
		h, a := twoMemberAutomation(t)
		h.forceState(a, func(a *Automation) {
			a.Status = AutomationBlocked
			a.BudgetState = &BudgetState{Action: BudgetActionBlocked, Scope: "automation", CeilingUSD: 5}
		})
		cur, _ := h.m.GetAutomation(a.ID)
		got, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{Clear: true}, "", cur.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != AutomationBlocked || got.BudgetState == nil || got.BudgetState.Action != BudgetActionBlocked {
			t.Fatalf("status %s state %+v: only the operator's resume may lift a block", got.Status, got.BudgetState)
		}
	})
}

func TestManagerSetBudgetVersionConflict(t *testing.T) {
	h, a := twoMemberAutomation(t)
	first, err := h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", a.Version)
	if err != nil {
		t.Fatal(err)
	}
	// a.Version is now stale.
	_, err = h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{CeilingType: "dollar", CeilingValue: 1, Window: "day"}, "", a.Version)
	wantErr(t, err, "409: ", "version")
	if !strings.HasPrefix(err.Error(), "409: ") {
		t.Fatalf("error %q must start with 409: ", err)
	}
	cur, _ := h.m.GetAutomation(a.ID)
	if cur.Version != first.Version || cur.BudgetPolicy.CeilingValue != 40 {
		t.Fatalf("a conflicting set wrote: version %d policy %+v", cur.Version, cur.BudgetPolicy)
	}
	// A stale caller learns of the conflict whatever the state, and for a member.
	_, err = h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m1", Clear: true}, "", a.Version)
	wantErr(t, err, "409: ")
	// Zero is not "any version".
	_, err = h.m.SetAutomationBudgetPolicy(a.ID, weekRequest(), "", 0)
	wantErr(t, err, "409: ")
	if n := len(h.auditRecords("automation_set_budget")); n != 1 {
		t.Fatalf("%d set_budget audit records, want only the successful one", n)
	}
}

func ptr[T any](v T) *T { return &v }
