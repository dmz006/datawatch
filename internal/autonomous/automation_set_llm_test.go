package autonomous

import (
	"strings"
	"testing"
)

// llmAuto is a draft two-member Automation in a control harness.
func llmAuto(t *testing.T) (*coordHarness, *Automation) {
	t.Helper()
	h, _ := newControlHarness(t, 2)
	a := h.makeAuto([][3]string{{"m1", "", ""}}, 2)
	return h, a
}

// setAutoStatus forces an Automation into status s.
func setAutoStatus(t *testing.T, h *coordHarness, a *Automation, s AutomationStatus) *Automation {
	t.Helper()
	out, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// reloadAutomation opens the on-disk store afresh and returns Automation id.
func reloadAutomation(t *testing.T, h *coordHarness, id string) *Automation {
	t.Helper()
	s, err := NewAutomationStore(h.m.store.dir, h.m.store.GetPRD)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(id)
	if !ok {
		t.Fatalf("automation %s missing after reload", id)
	}
	return got
}

func countDecisions(a *Automation, kind string) int {
	n := 0
	for _, d := range a.Decisions {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

func TestSetAutomationLLM(t *testing.T) {
	h, a := llmAuto(t)
	got, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), strp("claude-opus-5-5"), strp("high"), strp("pi-prompt"), strp("qwen3.8:27b"), "alice", a.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != a.Version+1 {
		t.Fatalf("version %d, want %d", got.Version, a.Version+1)
	}
	check := func(g *Automation) {
		t.Helper()
		if g.Backend != "claude-code" || g.Model != "claude-opus-5-5" || g.Effort != "high" ||
			g.PlanningBackend != "pi-prompt" || g.PlanningModel != "qwen3.8:27b" {
			t.Fatalf("fields not written: %+v", g)
		}
	}
	check(got)
	check(reloadAutomation(t, h, a.ID))
}

func TestSetAutomationVerificationLLM(t *testing.T) {
	h, a := llmAuto(t)
	if _, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), strp("claude-opus-5-5"), strp("high"), strp("pi-prompt"), strp("qwen3.8:27b"), "", 0); err != nil {
		t.Fatal(err)
	}
	before := h.get(a)
	got, err := h.m.SetAutomationVerificationLLM(a.ID, strp("claude-code"), strp("claude-sonnet-5-5"), strp("medium"), "bob", before.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != before.Version+1 {
		t.Fatalf("version %d, want %d", got.Version, before.Version+1)
	}
	check := func(g *Automation) {
		t.Helper()
		if g.VerificationBackend != "claude-code" || g.VerificationModel != "claude-sonnet-5-5" || g.VerificationEffort != "medium" {
			t.Fatalf("verification fields not written: %+v", g)
		}
		if g.Backend != before.Backend || g.Model != before.Model || g.Effort != before.Effort ||
			g.PlanningBackend != before.PlanningBackend || g.PlanningModel != before.PlanningModel {
			t.Fatalf("execution or planning fields changed: %+v", g)
		}
	}
	check(got)
	check(reloadAutomation(t, h, a.ID))
}

func TestSetAutomationLLMRejections(t *testing.T) {
	t.Run("completed and cancelled", func(t *testing.T) {
		for _, s := range []AutomationStatus{AutomationCompleted, AutomationCancelled} {
			h, a := llmAuto(t)
			a = setAutoStatus(t, h, a, s)
			_, err := h.m.SetAutomationLLM(a.ID, strp("pi-prompt"), nil, nil, nil, nil, "", a.Version)
			wantErr(t, err, "409: ", string(s))
			_, err = h.m.SetAutomationVerificationLLM(a.ID, strp("pi-prompt"), nil, nil, "", a.Version)
			wantErr(t, err, "409: ", string(s))
			if got := h.get(a); got.Version != a.Version || got.Backend != "" {
				t.Fatalf("%s automation was written: %+v", s, got)
			}
		}
	})
	t.Run("unknown", func(t *testing.T) {
		h, _ := llmAuto(t)
		_, err := h.m.SetAutomationLLM("nope", strp("pi-prompt"), nil, nil, nil, nil, "", 0)
		wantErr(t, err, "404: automation nope not found")
		_, err = h.m.SetAutomationVerificationLLM("nope", strp("pi-prompt"), nil, nil, "", 0)
		wantErr(t, err, "404: automation nope not found")
	})
	t.Run("stale version", func(t *testing.T) {
		h, a := llmAuto(t)
		_, err := h.m.SetAutomationLLM(a.ID, strp("pi-prompt"), nil, nil, nil, nil, "", a.Version+5)
		wantErr(t, err, "409: version mismatch")
		_, err = h.m.SetAutomationVerificationLLM(a.ID, strp("pi-prompt"), nil, nil, "", a.Version+5)
		wantErr(t, err, "409: version mismatch")
		if got := h.get(a); got.Version != a.Version {
			t.Fatalf("version moved to %d", got.Version)
		}
	})
	t.Run("mismatched pair", func(t *testing.T) {
		h, a := llmAuto(t)
		_, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), strp("anthropic/claude-haiku-5-5"), nil, nil, nil, "", 0)
		if err == nil || strings.Contains(err.Error(), "prd") {
			t.Fatalf("want a pair error that does not say prd, got %v", err)
		}
		_, err = h.m.SetAutomationVerificationLLM(a.ID, strp("claude-code"), strp("anthropic/claude-haiku-5-5"), nil, "", 0)
		if err == nil || strings.Contains(err.Error(), "prd") {
			t.Fatalf("want a pair error that does not say prd, got %v", err)
		}
		// A model change alone is checked against the stored backend.
		if _, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), nil, nil, nil, nil, "", 0); err != nil {
			t.Fatal(err)
		}
		_, err = h.m.SetAutomationLLM(a.ID, nil, strp("anthropic/claude-haiku-5-5"), nil, nil, nil, "", 0)
		if err == nil {
			t.Fatal("model alone was not checked against the stored backend")
		}
		if got := h.get(a); got.Model != "" {
			t.Fatalf("rejected model was written: %q", got.Model)
		}
	})
	t.Run("bad effort", func(t *testing.T) {
		h, a := llmAuto(t)
		_, err := h.m.SetAutomationLLM(a.ID, nil, nil, strp("extreme"), nil, nil, "", 0)
		wantErr(t, err, "effort", "extreme")
		_, err = h.m.SetAutomationVerificationLLM(a.ID, nil, nil, strp("extreme"), "", 0)
		wantErr(t, err, "effort", "extreme")
		if got := h.get(a); got.Version != a.Version {
			t.Fatalf("version moved to %d", got.Version)
		}
	})
}

func TestSetAutomationLLMPresenceSemantics(t *testing.T) {
	h, a := llmAuto(t)
	if _, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), strp("claude-opus-5-5"), strp("high"), strp("pi-prompt"), strp("qwen3.8:27b"), "", 0); err != nil {
		t.Fatal(err)
	}
	t.Run("nil leaves unchanged", func(t *testing.T) {
		got, err := h.m.SetAutomationLLM(a.ID, nil, strp("claude-sonnet-5-5"), nil, nil, nil, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.Backend != "claude-code" || got.Model != "claude-sonnet-5-5" || got.Effort != "high" ||
			got.PlanningBackend != "pi-prompt" || got.PlanningModel != "qwen3.8:27b" {
			t.Fatalf("unsupplied fields changed: %+v", got)
		}
	})
	t.Run("empty clears", func(t *testing.T) {
		got, err := h.m.SetAutomationLLM(a.ID, nil, strp(""), strp(""), nil, strp(""), "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.Model != "" || got.Effort != "" || got.PlanningModel != "" || got.Backend != "claude-code" || got.PlanningBackend != "pi-prompt" {
			t.Fatalf("clear wrong: %+v", got)
		}
	})
	t.Run("version zero skips the check", func(t *testing.T) {
		cur := h.get(a)
		got, err := h.m.SetAutomationLLM(a.ID, nil, nil, strp("low"), nil, nil, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != cur.Version+1 {
			t.Fatalf("version %d, want %d", got.Version, cur.Version+1)
		}
	})
	t.Run("planning leaves run pair alone", func(t *testing.T) {
		cur := h.get(a)
		got, err := h.m.SetAutomationLLM(a.ID, nil, nil, nil, strp("ollama"), strp("claude-opus-5-5"), "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.PlanningBackend != "ollama" || got.PlanningModel != "claude-opus-5-5" {
			t.Fatalf("planning pair not stored as given: %+v", got)
		}
		if got.Backend != cur.Backend || got.Model != cur.Model {
			t.Fatalf("planning change touched Backend/Model: %+v", got)
		}
	})
	t.Run("blocked and paused allowed", func(t *testing.T) {
		for _, s := range []AutomationStatus{AutomationBlocked, AutomationPaused, AutomationActive} {
			setAutoStatus(t, h, a, s)
			if _, err := h.m.SetAutomationVerificationLLM(a.ID, nil, nil, strp("high"), "", 0); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
	})
}

func TestSetAutomationLLMDecisionAndAudit(t *testing.T) {
	h, a := llmAuto(t)
	got, err := h.m.SetAutomationLLM(a.ID, strp("claude-code"), strp(""), nil, nil, strp("qwen3.8:27b"), "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if n := countDecisions(got, controlSetLLM); n != 1 {
		t.Fatalf("%d decisions of kind %s", n, controlSetLLM)
	}
	d := got.Decisions[len(got.Decisions)-1]
	if d.Kind != "automation_set_llm" || d.Actor != "alice" || d.Note != "backend=claude-code model= planning_model=qwen3.8:27b" {
		t.Fatalf("decision %+v", d)
	}
	det := h.auditDetail("automation_set_llm")
	if det == nil || det["automation_id"] != a.ID || det["actor"] != "alice" || det["version"] != got.Version ||
		det["backend"] != "claude-code" || det["model"] != "" || det["planning_model"] != "qwen3.8:27b" {
		t.Fatalf("audit %v", det)
	}
	for _, k := range []string{"effort", "planning_backend"} {
		if _, ok := det[k]; ok {
			t.Fatalf("audit lists unsupplied field %s: %v", k, det)
		}
	}

	got, err = h.m.SetAutomationVerificationLLM(a.ID, nil, strp("claude-sonnet-5-5"), strp("high"), "", got.Version)
	if err != nil {
		t.Fatal(err)
	}
	if n := countDecisions(got, controlSetVerificationLLM); n != 1 {
		t.Fatalf("%d decisions of kind %s", n, controlSetVerificationLLM)
	}
	d = got.Decisions[len(got.Decisions)-1]
	if d.Kind != "automation_set_verification_llm" || d.Actor != "operator" || d.Note != "verification_model=claude-sonnet-5-5 verification_effort=high" {
		t.Fatalf("decision %+v", d)
	}
	det = h.auditDetail("automation_set_verification_llm")
	if det == nil || det["automation_id"] != a.ID || det["actor"] != "operator" || det["version"] != got.Version ||
		det["verification_model"] != "claude-sonnet-5-5" || det["verification_effort"] != "high" {
		t.Fatalf("audit %v", det)
	}
	if _, ok := det["verification_backend"]; ok {
		t.Fatalf("audit lists unsupplied field: %v", det)
	}
}
