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
