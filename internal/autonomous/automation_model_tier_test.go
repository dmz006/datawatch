package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutomationPlanningFieldsRoundTrip(t *testing.T) {
	// JSON round trip keeps the planning pair.
	in := Automation{Name: "n", PlanningBackend: "pi-prompt", PlanningModel: "qwen3.8:27b"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Automation
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.PlanningBackend != "pi-prompt" || out.PlanningModel != "qwen3.8:27b" {
		t.Fatalf("round trip lost planning fields: %+v", out)
	}

	// Empty fields are omitted from the JSON.
	b, err = json.Marshal(Automation{Name: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "planning_") {
		t.Fatalf("empty planning fields should be omitted: %s", b)
	}

	// Create and Update through the Manager persist and return them,
	// independent of the run and verify defaults.
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := makeAutomationMemberPRDs(t, m, 1)[0]
	a, err := m.CreateAutomation(AutomationInput{
		Name:                "tiers",
		Members:             []AutomationMember{{PRDID: p.ID}},
		PlanningBackend:     "pi-prompt",
		PlanningModel:       "qwen3.8:27b",
		Backend:             "claude-code",
		Model:               "sonnet",
		VerificationBackend: "claude-code",
		VerificationModel:   "haiku",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	check := func(label string, a *Automation, pb, pm, b, mo, vb, vm string) {
		t.Helper()
		if a.PlanningBackend != pb || a.PlanningModel != pm || a.Backend != b || a.Model != mo ||
			a.VerificationBackend != vb || a.VerificationModel != vm {
			t.Fatalf("%s: got planning=%s/%s run=%s/%s verify=%s/%s", label,
				a.PlanningBackend, a.PlanningModel, a.Backend, a.Model, a.VerificationBackend, a.VerificationModel)
		}
	}
	check("create", a, "pi-prompt", "qwen3.8:27b", "claude-code", "sonnet", "claude-code", "haiku")
	got, ok := m.GetAutomation(a.ID)
	if !ok {
		t.Fatal("automation not found after create")
	}
	check("get", got, "pi-prompt", "qwen3.8:27b", "claude-code", "sonnet", "claude-code", "haiku")

	// Update changes only the planning pair; the others stay as supplied.
	upd, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:                a.Name,
		PlanningBackend:     "ollama",
		PlanningModel:       "llama3",
		Backend:             "claude-code",
		Model:               "sonnet",
		VerificationBackend: "claude-code",
		VerificationModel:   "haiku",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	check("update", upd, "ollama", "llama3", "claude-code", "sonnet", "claude-code", "haiku")

	// Clearing the planning pair leaves run and verify untouched.
	upd, err = m.UpdateAutomation(a.ID, upd.Version, AutomationInput{
		Name:                a.Name,
		Backend:             "claude-code",
		Model:               "sonnet",
		VerificationBackend: "claude-code",
		VerificationModel:   "haiku",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	check("clear", upd, "", "", "claude-code", "sonnet", "claude-code", "haiku")
}

func TestAutomationForPRDLookup(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prds := makeAutomationMemberPRDs(t, m, 3)
	standalone, member, nestedMember := prds[0], prds[1], prds[2]

	if got := m.AutomationForPRD(standalone); got != nil {
		t.Fatalf("standalone PRD: got %+v, want nil", got)
	}
	if got := m.AutomationForPRD(nil); got != nil {
		t.Fatalf("nil PRD: got %+v, want nil", got)
	}

	child, err := m.CreateAutomation(AutomationInput{
		Name:            "child",
		Members:         []AutomationMember{{PRDID: nestedMember.ID}},
		PlanningBackend: "pi-prompt",
		PlanningModel:   "child-model",
		Backend:         "pi-prompt",
		Model:           "child-run",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := m.CreateAutomation(AutomationInput{
		Name: "parent",
		Members: []AutomationMember{
			{PRDID: member.ID},
			{Kind: MemberKindAutomation, AutomationID: child.ID},
		},
		PlanningBackend: "claude-code",
		PlanningModel:   "parent-model",
		Backend:         "claude-code",
		Model:           "parent-run",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}

	// Re-read the PRDs so AutomationID reflects the synced ownership.
	member, _ = m.store.GetPRD(member.ID)
	nestedMember, _ = m.store.GetPRD(nestedMember.ID)

	got := m.AutomationForPRD(member)
	if got == nil || got.ID != parent.ID || got.PlanningModel != "parent-model" || got.Model != "parent-run" {
		t.Fatalf("member: got %+v, want parent %s with its defaults", got, parent.ID)
	}

	// The result is a copy: editing it does not change the stored Automation.
	got.PlanningModel = "mutated"
	got.Members[0].PRDID = "mutated"
	again := m.AutomationForPRD(member)
	if again.PlanningModel != "parent-model" || again.Members[0].PRDID != member.ID {
		t.Fatalf("stored automation changed through the copy: %+v", again)
	}

	// Ownership by the store alone resolves when prd.AutomationID is empty.
	bare := &PRD{ID: member.ID}
	if got := m.AutomationForPRD(bare); got == nil || got.ID != parent.ID {
		t.Fatalf("OwnerOf fallback: got %+v, want parent %s", got, parent.ID)
	}

	// A nested child's member resolves to the child only, with no inheritance.
	got = m.AutomationForPRD(nestedMember)
	if got == nil || got.ID != child.ID || got.PlanningModel != "child-model" || got.Model != "child-run" {
		t.Fatalf("nested member: got %+v, want child %s", got, child.ID)
	}

	// A Manager without an Automation store is safe.
	if got := (&Manager{}).AutomationForPRD(member); got != nil {
		t.Fatalf("nil store: got %+v, want nil", got)
	}
}
