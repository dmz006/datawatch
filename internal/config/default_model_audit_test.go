package config

import (
	"strings"
	"testing"
)

// TestNoShippedOpusDefault proves the shipped (non-operator) defaults never
// name an Opus-class model: the merge-pipeline default conflict model and
// every claude-code planner entry of the shipped example rules. It also
// proves the operator's choice is untouched: an Opus row the operator writes
// still passes validation and comes back byte-for-byte through the effective
// lookups.
func TestNoShippedOpusDefault(t *testing.T) {
	isOpus := func(m string) bool {
		return strings.Contains(strings.ToLower(m), "opus")
	}

	// 1. The shipped merge-pipeline default conflict model is non-empty
	// and non-Opus.
	d := MergePipelineDefaultConflictModel
	if d == "" {
		t.Fatal("MergePipelineDefaultConflictModel is empty")
	}
	if isOpus(d) {
		t.Fatalf("shipped default conflict model %q is an Opus model", d)
	}
	if got := (MergePipelineConfig{}).EffectiveConflictModel("any"); got != d {
		t.Fatalf("unset EffectiveConflictModel = %q, want the shipped default %q", got, d)
	}

	// 2. Every claude-code planner entry of the shipped example rules is
	// non-Opus (the escalation target on another backend is out of scope).
	ex := ExampleRules()
	if len(ex) == 0 {
		t.Fatal("ExampleRules is empty")
	}
	for _, r := range ex {
		for _, p := range r.Planners {
			if p.Backend != "claude-code" {
				continue
			}
			if isOpus(p.Model) {
				t.Fatalf("example rule %q ships an Opus claude-code planner %q", r.Name, p.Model)
			}
		}
	}

	// 3. An operator-written Opus row still validates and passes through
	// byte-for-byte.
	opus := "claude-opus-5-5" // an Opus model name an operator might write

	// Merge pipeline: the operator names Opus as the conflict model.
	mp := MergePipelineConfig{
		ConflictPolicy: ConflictPolicyMechanicalAndModel,
		ConflictModel:  &opus,
	}
	if err := mp.Validate(); err != nil {
		t.Fatalf("operator Opus conflict model refused by validation: %v", err)
	}
	if got := mp.EffectiveConflictModel("any"); got != opus {
		t.Fatalf("EffectiveConflictModel = %q, want the operator's %q untouched", got, opus)
	}
	policy, model, why := mp.ResolveConflictPolicy("any", fakeLLMs{opus: true})
	if policy != ConflictPolicyMechanicalAndModel || model != opus || why != "" {
		t.Fatalf("ResolveConflictPolicy = (%q, %q, %q), want (%q, %q, %q)",
			policy, model, why, ConflictPolicyMechanicalAndModel, opus, "")
	}

	// Planning routing: the operator names Opus as a planner.
	rr := PlanningRule{
		Name:     "operator-strong",
		Planners: []PlannerRef{{Backend: "claude-code", Model: opus}},
	}
	pr := PlanningRoutingConfig{Enabled: true, Rules: []PlanningRule{rr}}
	if err := ValidatePlanningRouting(pr); err != nil {
		t.Fatalf("operator Opus planning row refused by validation: %v", err)
	}
	if got := pr.Rules[0].Planners[0].Model; got != opus {
		t.Fatalf("operator Opus planner row came back as %q, want %q untouched", got, opus)
	}
}
