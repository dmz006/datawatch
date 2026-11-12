package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func validAssistant() *DecisionAssistant {
	return &DecisionAssistant{Backend: "pi-prompt", Model: "qwen3.8:27b", Hooks: []string{DecisionHookEmptyEdge}}
}

func TestDecisionAssistantValidation(t *testing.T) {
	if err := validateDecisionAssistant(nil); err != nil {
		t.Fatalf("nil must be valid: %v", err)
	}
	if err := validateDecisionAssistant(validAssistant()); err != nil {
		t.Fatalf("valid assistant refused: %v", err)
	}
	cases := map[string]func(d *DecisionAssistant){
		"backend is required": func(d *DecisionAssistant) { d.Backend = "" },
		"model is required":   func(d *DecisionAssistant) { d.Model = "" },
		"unknown mode":        func(d *DecisionAssistant) { d.Mode = "auto" },
		"unknown hook":        func(d *DecisionAssistant) { d.Hooks = []string{"bogus"} },
		"want a value in":     func(d *DecisionAssistant) { d.Thresholds = map[string]float64{DecisionHookEmptyEdge: 0} },
		"threshold for unknown hook": func(d *DecisionAssistant) {
			d.Thresholds = map[string]float64{"bogus": 0.5}
		},
		"does not pair": func(d *DecisionAssistant) { d.Backend, d.Model = "pi-prompt", "claude-opus-5-5" },
	}
	for want, mutate := range cases {
		d := validAssistant()
		mutate(d)
		err := validateDecisionAssistant(d)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	for _, bad := range []float64{-0.1, 1.01} {
		d := validAssistant()
		d.Thresholds = map[string]float64{DecisionHookBudgetBreach: bad}
		if validateDecisionAssistant(d) == nil {
			t.Errorf("threshold %v accepted", bad)
		}
	}
	d := validAssistant()
	d.Mode = DecisionModeLive
	d.Thresholds = map[string]float64{DecisionHookBudgetBreach: 1}
	if err := validateDecisionAssistant(d); err != nil {
		t.Fatalf("live mode with threshold 1 refused: %v", err)
	}
}

func TestDecisionAssistantDefaults(t *testing.T) {
	d := validAssistant()
	if d.EffectiveMode() != DecisionModeShadow {
		t.Fatalf("mode %q, want shadow", d.EffectiveMode())
	}
	for _, h := range []string{DecisionHookEmptyEdge, DecisionHookSkipMember, DecisionHookInsertMember} {
		if got := d.Threshold(h); got != 0.9 {
			t.Errorf("%s threshold %v, want 0.9", h, got)
		}
	}
	if got := d.Threshold(DecisionHookBudgetBreach); got != 0.95 {
		t.Errorf("budget_breach threshold %v, want 0.95", got)
	}
	d.Thresholds = map[string]float64{DecisionHookSkipMember: 0.5}
	if d.Threshold(DecisionHookSkipMember) != 0.5 || d.Threshold(DecisionHookEmptyEdge) != 0.9 {
		t.Fatal("override must replace only its own hook")
	}
	if !d.HookEnabled(DecisionHookEmptyEdge) || d.HookEnabled(DecisionHookSkipMember) {
		t.Fatal("HookEnabled must follow Hooks")
	}
}

func TestDecisionAssistantPrecedence(t *testing.T) {
	auto := validAssistant()
	mem := &DecisionAssistant{Backend: "pi-prompt", Model: "other", Mode: DecisionModeLive}
	a := &Automation{DecisionAssistant: auto, Members: []AutomationMember{{ID: "m1", DecisionAssistant: mem}, {ID: "m2"}}}
	if got := EffectiveDecisionAssistant(a, &a.Members[0]); got != mem {
		t.Fatal("member setting must win")
	}
	if got := EffectiveDecisionAssistant(a, &a.Members[1]); got != auto {
		t.Fatal("member without a setting must inherit the Automation's")
	}
	if got := EffectiveDecisionAssistant(a, nil); got != auto {
		t.Fatal("nil member resolves at the Automation level")
	}
}

func TestDecisionAssistantOffByDefault(t *testing.T) {
	a := &Automation{Members: []AutomationMember{{ID: "m1"}}}
	if EffectiveDecisionAssistant(a, &a.Members[0]) != nil {
		t.Fatal("unset must resolve to off")
	}
	var d *DecisionAssistant
	if d.HookEnabled(DecisionHookEmptyEdge) || d.EffectiveMode() != DecisionModeShadow {
		t.Fatal("nil assistant must have no hooks enabled")
	}
	b, _ := json.Marshal(a)
	if strings.Contains(string(b), "decision_assistant") {
		t.Fatalf("unset field must be omitted: %s", b)
	}
}

func TestDecisionAssistantJSONRoundTrip(t *testing.T) {
	h, a := llmAuto(t)
	da := validAssistant()
	da.Mode = DecisionModeLive
	da.Thresholds = map[string]float64{DecisionHookEmptyEdge: 0.8}
	if _, err := h.m.SetDecisionAssistant(a.ID, "m0", "alice", da, a.Version); err != nil {
		t.Fatal(err)
	}
	cur, _ := h.m.GetAutomation(a.ID)
	if _, err := h.m.SetDecisionAssistant(a.ID, "", "alice", da, cur.Version); err != nil {
		t.Fatal(err)
	}
	got := reloadAutomation(t, h, a.ID)
	if got.DecisionAssistant == nil || got.DecisionAssistant.Thresholds[DecisionHookEmptyEdge] != 0.8 || got.DecisionAssistant.Mode != DecisionModeLive {
		t.Fatalf("automation-level setting lost on reload: %+v", got.DecisionAssistant)
	}
	if got.Members[0].DecisionAssistant == nil || got.Members[0].DecisionAssistant.Model != "qwen3.8:27b" {
		t.Fatalf("member setting lost on reload: %+v", got.Members[0].DecisionAssistant)
	}
	// An update that omits the field keeps it, and a member sent without it keeps its own.
	upd := AutomationInput{Name: "renamed", Members: got.Members}
	upd.Members = append([]AutomationMember(nil), got.Members...)
	upd.Members[0].DecisionAssistant = nil
	out, err := h.m.UpdateAutomation(a.ID, got.Version, upd, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if out.DecisionAssistant == nil || out.Members[0].DecisionAssistant == nil {
		t.Fatal("update dropped the decision assistant")
	}
	// Create round-trips the field too.
	created, err := h.m.CreateAutomation(AutomationInput{Name: "with-da", DecisionAssistant: da}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if reloadAutomation(t, h, created.ID).DecisionAssistant == nil {
		t.Fatal("create dropped the decision assistant")
	}
}

func TestDecisionAssistantSetRecordsDecisionAndAudit(t *testing.T) {
	h, a := llmAuto(t)
	if _, err := h.m.SetDecisionAssistant(a.ID, "", "alice", &DecisionAssistant{Backend: "pi-prompt", Model: "claude-opus-5-5"}, a.Version); err == nil {
		t.Fatal("mismatched pair accepted")
	}
	if cur, _ := h.m.GetAutomation(a.ID); cur.Version != a.Version || cur.DecisionAssistant != nil {
		t.Fatal("refused call must not write")
	}
	if _, err := h.m.SetDecisionAssistant(a.ID, "nope", "alice", validAssistant(), a.Version); err == nil {
		t.Fatal("unknown member accepted")
	}
	got, err := h.m.SetDecisionAssistant(a.ID, "", "alice", validAssistant(), a.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != a.Version+1 {
		t.Fatalf("version %d, want %d", got.Version, a.Version+1)
	}
	if n := countDecisions(got, "automation_set_decision_assistant"); n != 1 {
		t.Fatalf("%d decisions, want 1", n)
	}
	last := got.Decisions[len(got.Decisions)-1]
	if last.Actor != "alice" || !strings.Contains(last.Note, "mode=shadow") {
		t.Fatalf("decision %+v", last)
	}
	n := 0
	for _, act := range h.audits {
		if act == "automation_set_decision_assistant" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d audit records, want 1", n)
	}
	// Clearing turns it off again.
	cleared, err := h.m.SetDecisionAssistant(a.ID, "", "", nil, 0)
	if err != nil || cleared.DecisionAssistant != nil {
		t.Fatalf("clear: %v %+v", err, cleared.DecisionAssistant)
	}
	if _, err := h.m.SetDecisionAssistant(a.ID, "", "alice", validAssistant(), 999); err == nil {
		t.Fatal("stale version accepted")
	}
}
