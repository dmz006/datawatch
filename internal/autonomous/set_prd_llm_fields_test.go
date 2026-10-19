// Presence-aware tests for Manager.SetPRDLLM:
//
//  - nil *string  → field is left unchanged (omitted)
//  - "" *string   → field is cleared (explicitly set to empty)
//  - non-empty    → field is replaced
//
// The decision log note of a set_prd_llm entry must mention only the
// fields actually included in that call.

package autonomous

import (
	"strings"
	"testing"
)

type prdLLMField struct {
	name    string
	initial string
}

// prdLLMTestFields lists the five PRD LLM fields with distinct
// non-empty seed values.
func prdLLMTestFields() []prdLLMField {
	return []prdLLMField{
		{"backend", "ollama-seed"},
		{"effort", "high"},
		{"model", "worker-model-seed"},
		{"decomposition_profile", "plan-profile-seed"},
		{"decomposition_model", "plan-model-seed"},
	}
}

// newManagerForPRDLLM creates a fresh test manager.
func newManagerForPRDLLM(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// seedPRDLLM creates a PRD in m and seeds all five PRD LLM fields to
// the distinct non-empty values in prdLLMTestFields, verifying the
// round-trip before returning the PRD ID.
func seedPRDLLM(t *testing.T, m *Manager) string {
	t.Helper()
	prd, err := m.CreatePRD("test spec", t.TempDir(), "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	fields := prdLLMTestFields()
	if _, err := m.SetPRDLLM(prd.ID, sp(fields[0].initial), sp(fields[1].initial), sp(fields[2].initial), sp(fields[3].initial), sp(fields[4].initial), "seed"); err != nil {
		t.Fatalf("SetPRDLLM (seed): %v", err)
	}
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found in store after seed")
	}
	for i, f := range fields {
		if got := prdLLMFieldAt(stored, i); got != f.initial {
			t.Fatalf("seed %s: got %q, want %q", f.name, got, f.initial)
		}
	}
	return prd.ID
}

// prdLLMFieldAt returns the stored value of the i-th PRD LLM field
// (index order = prdLLMTestFields).
func prdLLMFieldAt(prd *PRD, i int) string {
	switch i {
	case 0:
		return prd.Backend
	case 1:
		return string(prd.Effort)
	case 2:
		return prd.Model
	case 3:
		return prd.DecompositionProfile
	case 4:
		return prd.DecompositionModel
	}
	return ""
}

// setOnePRDField calls SetPRDLLM with exactly the i-th field set to
// value and the other four nil.
func setOnePRDField(t *testing.T, m *Manager, prdID string, i int, value string) {
	t.Helper()
	var b, e, mo, dp, dm *string
	switch i {
	case 0:
		b = sp(value)
	case 1:
		e = sp(value)
	case 2:
		mo = sp(value)
	case 3:
		dp = sp(value)
	case 4:
		dm = sp(value)
	default:
		t.Fatalf("setOnePRDField: bad index %d", i)
	}
	if _, err := m.SetPRDLLM(prdID, b, e, mo, dp, dm, "test"); err != nil {
		t.Fatalf("SetPRDLLM (one field): %v", err)
	}
}

// loadedPRDLLMFields reloads the PRD and returns the current value of
// each of the five fields.
func loadedPRDLLMFields(t *testing.T, m *Manager, prdID string) []string {
	t.Helper()
	stored, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("PRD not found in store")
	}
	out := make([]string, 5)
	for i := range out {
		out[i] = prdLLMFieldAt(stored, i)
	}
	return out
}

// lastSetPRDLLMNote returns the Note of the most recent decision-log
// entry of kind set_prd_llm.
func lastSetPRDLLMNote(t *testing.T, m *Manager, prdID string) string {
	t.Helper()
	stored, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("PRD not found in store")
	}
	found, note := false, ""
	for _, d := range stored.Decisions {
		if d.Kind == "set_prd_llm" {
			found = true
			note = d.Note
		}
	}
	if !found {
		t.Fatalf("no set_prd_llm decision entry found; decisions: %v", stored.Decisions)
	}
	return note
}

func TestSetPRDLLM_FieldOmitted_Kept(t *testing.T) {
	m := newManagerForPRDLLM(t)
	fields := prdLLMTestFields()
	updates := map[int]string{
		0: "ollama-updated",
		1: "low",
		2: "worker-model-updated",
		3: "plan-profile-updated",
		4: "plan-model-updated",
	}

	// (a) For each field, in its own subtest with a fresh seeded PRD:
	// a call with only that field non-nil (the other four nil) must
	// update it and leave the other four byte-for-byte unchanged.
	for i, f := range fields {
		t.Run("Omits:Others_"+f.name, func(t *testing.T) {
			prdID := seedPRDLLM(t, m)
			setOnePRDField(t, m, prdID, i, updates[i])
			got := loadedPRDLLMFields(t, m, prdID)
			if got[i] != updates[i] {
				t.Errorf("%s: got %q, want %q", f.name, got[i], updates[i])
			}
			for j := range fields {
				if j == i {
					continue
				}
				if got[j] != fields[j].initial {
					t.Errorf("%s: got %q, want unchanged %q", fields[j].name, got[j], fields[j].initial)
				}
			}
		})
	}

	// (b) A call with all five fields nil must not change any field.
	t.Run("Omits:All", func(t *testing.T) {
		prdID := seedPRDLLM(t, m)
		if _, err := m.SetPRDLLM(prdID, nil, nil, nil, nil, nil, "test"); err != nil {
			t.Fatalf("SetPRDLLM (all nil): %v", err)
		}
		got := loadedPRDLLMFields(t, m, prdID)
		for i, f := range fields {
			if got[i] != f.initial {
				t.Errorf("%s: got %q, want unchanged %q", f.name, got[i], f.initial)
			}
		}
	})

	// Decision log: after a call that sets only decomposition_profile,
	// the latest set_prd_llm entry mentions decomposition_profile and
	// NOT backend/effort/model.
	t.Run("DecisionLog_OnlyMentionsIncludedFields", func(t *testing.T) {
		prdID := seedPRDLLM(t, m)
		setOnePRDField(t, m, prdID, 3, "plan-profile-logcheck")
		note := lastSetPRDLLMNote(t, m, prdID)
		if !strings.Contains(note, "decomposition_profile") {
			t.Errorf("note should mention decomposition_profile; got %q", note)
		}
		for _, bad := range []string{"backend", "effort", "model"} {
			if strings.Contains(note, bad) {
				t.Errorf("note should NOT mention %q; got %q", bad, note)
			}
		}
	})
}

func TestSetPRDLLM_FieldEmpty_Cleared(t *testing.T) {
	m := newManagerForPRDLLM(t)
	fields := prdLLMTestFields()

	empty := ""
	for i, f := range fields {
		t.Run("Clears_"+f.name, func(t *testing.T) {
			prdID := seedPRDLLM(t, m)
			setOnePRDField(t, m, prdID, i, empty)
			got := loadedPRDLLMFields(t, m, prdID)
			if got[i] != "" {
				t.Errorf("%s: got %q, want cleared (empty)", f.name, got[i])
			}
			for j := range fields {
				if j == i {
					continue
				}
				if got[j] != fields[j].initial {
					t.Errorf("%s: got %q, want unchanged %q", fields[j].name, got[j], fields[j].initial)
				}
			}
		})
	}
}

func TestSetPRDLLM_FieldSet_Replaces(t *testing.T) {
	m := newManagerForPRDLLM(t)
	fields := prdLLMTestFields()

	newValues := []string{
		"ollama-new",
		"max",
		"worker-model-new",
		"plan-profile-new",
		"plan-model-new",
	}
	for i, v := range newValues {
		if v == fields[i].initial {
			t.Fatalf("newValues[%d] %q must differ from seed %q", i, v, fields[i].initial)
		}
	}

	prdID := seedPRDLLM(t, m)
	if _, err := m.SetPRDLLM(prdID, sp(newValues[0]), sp(newValues[1]), sp(newValues[2]), sp(newValues[3]), sp(newValues[4]), "test"); err != nil {
		t.Fatalf("SetPRDLLM (all set): %v", err)
	}

	got := loadedPRDLLMFields(t, m, prdID)
	for i, f := range fields {
		t.Run("Replaces_"+f.name, func(t *testing.T) {
			if got[i] != newValues[i] {
				t.Errorf("%s: got %q, want %q", f.name, got[i], newValues[i])
			}
		})
	}
}
