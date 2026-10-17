package autonomous

import (
	"strings"
	"testing"
)

func automaton(t *testing.T) *Automation {
	t.Helper()
	m := AutomationMember{ID: "m1", PRDID: "p1"}
	m2 := AutomationMember{ID: "m2", PRDID: "p2"}
	m.OnComplete = "m2"
	return &Automation{
		ID:            "a1",
		Name:          "seq",
		Status:        AutomationDraft,
		EntryMemberID: "m1",
		Members:       []AutomationMember{m, m2},
	}
}

func TestValidateAutomationValid(t *testing.T) {
	if err := validateAutomation(automaton(t)); err != nil {
		t.Fatalf("valid Automation rejected: %v", err)
	}
	// A draft may be created empty: no members, no entry.
	empty := &Automation{Name: "draft", Status: AutomationDraft}
	if err := validateAutomation(empty); err != nil {
		t.Fatalf("empty draft rejected: %v", err)
	}
}

func TestValidateAutomationIdentity(t *testing.T) {
	if err := validateAutomation(nil); err == nil {
		t.Fatal("nil Automation accepted")
	}
	a := automaton(t)
	a.Name = "  "
	if err := validateAutomation(a); err == nil || !strings.HasPrefix(err.Error(), "automation invalid: ") {
		t.Fatalf("blank name: got %v", err)
	}
	a = automaton(t)
	a.Status = ""
	if err := validateAutomation(a); err == nil {
		t.Fatal("empty status accepted")
	}
	a = automaton(t)
	a.Status = AutomationStatus("running")
	if err := validateAutomation(a); err == nil {
		t.Fatal("unknown status accepted")
	}
}

func TestValidateAutomationMembers(t *testing.T) {
	a := automaton(t)
	a.Members[0].ID = ""
	if err := validateAutomation(a); err == nil {
		t.Fatal("empty member ID accepted")
	}
	a = automaton(t)
	a.Members[0].PRDID = ""
	if err := validateAutomation(a); err == nil {
		t.Fatal("empty PRDID accepted")
	}
	a = automaton(t)
	a.Members[1].ID = "m1"
	a.EntryMemberID = "m1"
	if err := validateAutomation(a); err == nil {
		t.Fatal("duplicate member ID accepted")
	}
	a = automaton(t)
	a.Members[1].PRDID = "p1"
	if err := validateAutomation(a); err == nil {
		t.Fatal("same PRD in two members accepted")
	}
}

func TestValidateAutomationEntryAndEdges(t *testing.T) {
	a := automaton(t)
	a.EntryMemberID = ""
	if err := validateAutomation(a); err == nil {
		t.Fatal("missing entry member accepted")
	}
	a = automaton(t)
	a.EntryMemberID = "mx"
	if err := validateAutomation(a); err == nil {
		t.Fatal("unknown entry member accepted")
	}
	a = automaton(t)
	a.Members[0].OnComplete = "mx"
	if err := validateAutomation(a); err == nil {
		t.Fatal("edge to unknown member accepted")
	}
	a = automaton(t)
	a.Members[0].OnBlocked = "m1"
	if err := validateAutomation(a); err == nil {
		t.Fatal("self edge accepted")
	}
}

func TestValidateAutomationCycleAndReachability(t *testing.T) {
	// A straight line is valid, in any non-draft status.
	a := automaton(t)
	a.Status = AutomationActive
	if err := validateAutomation(a); err != nil {
		t.Fatalf("linear non-draft rejected: %v", err)
	}

	// OnComplete loop.
	a = automaton(t)
	a.Members[1].OnComplete = "m1"
	err := validateAutomation(a)
	if err == nil || !strings.Contains(err.Error(), "cycle detected via members [m1 m2]") {
		t.Fatalf("on_complete loop: got %v", err)
	}

	// An OnFailed edge looping back to an earlier member counts too.
	a = automaton(t)
	a.Members[1].OnFailed = "m1"
	if err = validateAutomation(a); err == nil || !strings.Contains(err.Error(), "cycle detected via members [m1 m2]") {
		t.Fatalf("on_failed loop: got %v", err)
	}

	// Multi-member cycle.
	a = automaton(t)
	a.Members = append(a.Members, AutomationMember{ID: "m3", PRDID: "p3"})
	a.Members[1].OnComplete = "m3"
	a.Members[2].OnBlocked = "m1"
	if err = validateAutomation(a); err == nil || !strings.Contains(err.Error(), "cycle detected via members [m1 m2 m3]") {
		t.Fatalf("three-member loop: got %v", err)
	}

	// Unreachable member: rejected on non-draft, tolerated on draft.
	a = automaton(t)
	a.Status = AutomationActive
	a.Members = append(a.Members, AutomationMember{ID: "m9", PRDID: "p9"})
	if err = validateAutomation(a); err == nil || !strings.Contains(err.Error(), "members unreachable from entry [m9]") {
		t.Fatalf("unreachable member: got %v", err)
	}
	a.Status = AutomationDraft
	if err = validateAutomation(a); err != nil {
		t.Fatalf("draft with unreachable member rejected: %v", err)
	}

	// Reachability follows all three edge kinds.
	a = automaton(t)
	a.Members[0].OnComplete = ""
	a.Members[0].OnBlocked = "m2"
	if err = validateAutomation(a); err != nil {
		t.Fatalf("on_blocked reachability: got %v", err)
	}

	if got := automationUnreachable(a); len(got) != 0 {
		t.Fatalf("automationUnreachable: got %v", got)
	}
}

func TestAutomationUnreachableSorted(t *testing.T) {
	a := automaton(t)
	a.Members = append(a.Members, AutomationMember{ID: "zb", PRDID: "pz"}, AutomationMember{ID: "aa", PRDID: "pa"})
	got := automationUnreachable(a)
	if len(got) != 2 || got[0] != "aa" || got[1] != "zb" {
		t.Fatalf("sorted unreachable: got %v", got)
	}
}

func TestValidateAutomationOwnership(t *testing.T) {
	lookup := func(id string) (*PRD, bool) { return &PRD{ID: id}, id == "p1" || id == "p2" }
	if err := validateAutomationOwnership(automaton(t), lookup, func(string) string { return "" }); err != nil {
		t.Fatalf("no owners: %v", err)
	}
	// Terminal Automations own nothing, even when someone else claims the PRD.
	for _, status := range []AutomationStatus{AutomationCompleted, AutomationCancelled} {
		a := automaton(t)
		a.Status = status
		if err := validateAutomationOwnership(a, lookup, func(string) string { return "a9" }); err != nil {
			t.Fatalf("terminal status %s: %v", status, err)
		}
	}
	// A nil Automation owns nothing.
	if err := validateAutomationOwnership(nil, lookup, func(string) string { return "a9" }); err != nil {
		t.Fatalf("nil Automation: %v", err)
	}
	// A PRD owned by this Automation is its own, which is allowed.
	if err := validateAutomationOwnership(automaton(t), lookup, func(id string) string {
		if id == "p2" {
			return "a1"
		}
		return ""
	}); err != nil {
		t.Fatalf("self ownership: %v", err)
	}
	a := automaton(t)
	a.Members[0].PRDID = "px"
	if err := validateAutomationOwnership(a, lookup, func(string) string { return "" }); err == nil || err.Error() != "automation invalid: member prd px not found" {
		t.Fatalf("missing prd: got %v", err)
	}
	lookupT := func(id string) (*PRD, bool) {
		p := &PRD{ID: id}
		if id == "p1" {
			p.IsTemplate = true
		}
		return p, true
	}
	if err := validateAutomationOwnership(automaton(t), lookupT, func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "is a template and cannot be a member") {
		t.Fatalf("template prd: got %v", err)
	}
	lookupC := func(id string) (*PRD, bool) {
		p := &PRD{ID: id}
		if id == "p1" {
			p.ParentPRDID = "pp"
		}
		return p, true
	}
	if err := validateAutomationOwnership(automaton(t), lookupC, func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "is a child automaton") {
		t.Fatalf("child prd: got %v", err)
	}
	if err := validateAutomationOwnership(automaton(t), lookup, func(string) string { return "a9" }); err == nil || err.Error() != "automation invalid: prd p1 already belongs to automation a9" {
		t.Fatalf("owned by another: got %v", err)
	}
}

func TestValidateAutomationBudgetPolicy(t *testing.T) {
	base := func() *BudgetPolicy {
		return &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "automation", Pacing: "none", OnBreach: "pause"}
	}
	if err := validateBudgetPolicy(nil); err != nil {
		t.Fatalf("nil policy rejected: %v", err)
	}
	if err := validateBudgetPolicy(base()); err != nil {
		t.Fatalf("good policy rejected: %v", err)
	}
	for name, mutate := range map[string]func(*BudgetPolicy){
		"ceiling type":  func(p *BudgetPolicy) { p.CeilingType = "cents" },
		"ceiling value": func(p *BudgetPolicy) { p.CeilingValue = 0 },
		"window":        func(p *BudgetPolicy) { p.Window = "year" },
		"pacing":        func(p *BudgetPolicy) { p.Pacing = "burst" },
		"on breach":     func(p *BudgetPolicy) { p.OnBreach = "alert" },
	} {
		p := base()
		mutate(p)
		a := automaton(t)
		a.BudgetPolicy = p
		if err := validateAutomation(a); err == nil {
			t.Fatalf("%s: bad automation policy accepted", name)
		}
		a = automaton(t)
		a.Members[0].BudgetPolicy = p
		if err := validateAutomation(a); err == nil {
			t.Fatalf("%s: bad member policy accepted", name)
		}
	}
}
