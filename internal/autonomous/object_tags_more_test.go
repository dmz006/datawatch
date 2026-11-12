package autonomous

import (
	"reflect"
	"testing"
)

// TestObjectTagsForDerivedRecordKinds pins the derived system tags a
// record kind takes once it carries one (section 12 of
// docs/plans/2026-11-06-automata-pages-redesign.md, and its draft task's
// extension to decision records, held questions and templates): a decision
// record and a held question take the fact of the Automaton their PRDID
// names (its project, its Automation and itself); a template takes the
// fact of the Automaton it was cloned from, when it records one. Each with
// no Automaton to name — unset, or gone by the time the read happens —
// takes no tags, and naming a missing Automaton is not an error.
func TestObjectTagsForDerivedRecordKinds(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// An Automaton inside an Automation, in a project directory: the
	// named Automaton's fact is project + Automation + itself.
	prd, err := m.CreatePRD("tag the records", "project/example", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "tag arc",
		Members: []AutomationMember{{PRDID: prd.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	wanted := []string{"automation:" + auto.ID, "automaton:" + fresh.ID, "project:example"}
	gone := "pr-does-not-exist" // a record can outlive its Automaton

	cases := []struct {
		name string
		fn   func() []string
		want []string
	}{
		{"decision record naming its Automaton",
			func() []string { return m.ObjectTagsForDecisionRecord(&DecisionRecord{ID: "dr-1", PRDID: fresh.ID}) }, wanted},
		{"decision record naming no Automaton",
			func() []string { return m.ObjectTagsForDecisionRecord(&DecisionRecord{ID: "dr-2"}) }, nil},
		{"decision record whose Automaton is gone",
			func() []string { return m.ObjectTagsForDecisionRecord(&DecisionRecord{ID: "dr-3", PRDID: gone}) }, nil},
		{"held question naming its Automaton",
			func() []string { return m.ObjectTagsForQuestion(&Question{ID: "q-1", PRDID: fresh.ID}) }, wanted},
		{"held question naming no Automaton",
			func() []string { return m.ObjectTagsForQuestion(&Question{ID: "q-2"}) }, nil},
		{"held question whose Automaton is gone",
			func() []string { return m.ObjectTagsForQuestion(&Question{ID: "q-3", PRDID: gone}) }, nil},
		{"template cloned from an Automaton",
			func() []string { return m.ObjectTagsForTemplate(&Template{ID: "tpl-1", SourcePRDID: fresh.ID}) }, wanted},
		{"template with no recorded source",
			func() []string { return m.ObjectTagsForTemplate(&Template{ID: "tpl-2"}) }, nil},
		{"template whose source Automaton is gone",
			func() []string { return m.ObjectTagsForTemplate(&Template{ID: "tpl-3", SourcePRDID: gone}) }, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn()
			// no tags is no tags, whether the derivation returns nil or an
			// empty slice.
			match := len(got) == len(tc.want) && (len(got) == 0 || reflect.DeepEqual(got, tc.want))
			if !match {
				t.Fatalf("tags = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestObjectTagsForDerivedRecordKinds_Facts pins the fact set per kind
// (not just the rendered tags): a record naming an Automaton takes that
// Automaton's whole object fact, and one naming none has no axes at all —
// which is what makes the per-axis counts on the page honest (no
// invented default).
func TestObjectTagsForDerivedRecordKinds_Facts(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("facts fixture", "project/example", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "tag arc",
		Members: []AutomationMember{{PRDID: prd.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	autoFacts := ObjectTagFacts{
		Project:    "example",
		Automation: auto.ID,
		Automaton:  fresh.ID,
	}

	cases := []struct {
		name string
		got  ObjectTagFacts
		want ObjectTagFacts
	}{
		{"decision record naming its Automaton",
			m.ObjectTagFactsForDecisionRecord(&DecisionRecord{ID: "dr-1", PRDID: fresh.ID}), autoFacts},
		{"held question naming its Automaton",
			m.ObjectTagFactsForQuestion(&Question{ID: "q-1", PRDID: fresh.ID}), autoFacts},
		{"template cloned from an Automaton",
			m.ObjectTagFactsForTemplate(&Template{ID: "tpl-1", SourcePRDID: fresh.ID}), autoFacts},
		{"decision record naming no Automaton",
			m.ObjectTagFactsForDecisionRecord(&DecisionRecord{ID: "dr-2"}), ObjectTagFacts{}},
		{"held question naming no Automaton",
			m.ObjectTagFactsForQuestion(&Question{ID: "q-2"}), ObjectTagFacts{}},
		{"template with no recorded source",
			m.ObjectTagFactsForTemplate(&Template{ID: "tpl-2"}), ObjectTagFacts{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Fatalf("facts %+v, want %+v", tc.got, tc.want)
			}
		})
	}
}
