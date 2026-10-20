package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJudgeOutputFile creates a judge output file under a temp dir and
// returns its path.
func writeJudgeOutputFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "judge-7f3a.out")
	if err := os.WriteFile(p, []byte("verdict body"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestRecordGuardrailVerdict_FailedSaveKeepsOutputFile(t *testing.T) {
	m, prd := newBL416Manager(t)
	m.savePRDHook = func(*PRD) error { return errors.New("disk full") }
	out := writeJudgeOutputFile(t)
	v := GuardrailVerdict{Guardrail: "rules", Outcome: "block", JudgeSessionID: "judge-7f3a", JudgeOutputPath: out}

	if err := m.recordGuardrailVerdict(prd, "t1", v); err == nil {
		t.Fatal("recordGuardrailVerdict: want save error, got nil")
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("judge output removed despite failed save: %v", err)
	}
}

func TestRecordGuardrailVerdict_SuccessfulSaveRemovesOutputFile(t *testing.T) {
	m, prd := newBL416Manager(t)
	out := writeJudgeOutputFile(t)
	v := GuardrailVerdict{Guardrail: "rules", Outcome: "pass", JudgeSessionID: "judge-7f3a", JudgeOutputPath: out}

	if err := m.recordGuardrailVerdict(prd, "t1", v); err != nil {
		t.Fatalf("recordGuardrailVerdict: %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("judge output still present after successful save: err=%v", err)
	}
}

func TestRecordGuardrailVerdict_DecisionEntry(t *testing.T) {
	m, prd := newBL416Manager(t)
	v := GuardrailVerdict{
		Guardrail:      "security",
		Outcome:        "block",
		Category:       "content",
		Summary:        "hardcoded token in config",
		JudgeSessionID: "judge-7f3a",
	}

	if err := m.recordGuardrailVerdict(prd, "t1", v); err != nil {
		t.Fatalf("recordGuardrailVerdict: %v", err)
	}
	if len(prd.Decisions) != 1 {
		t.Fatalf("Decisions: got %d entries, want 1", len(prd.Decisions))
	}
	d := prd.Decisions[0]
	if d.Kind != "guardrail_verdict" || d.Actor != "autonomous" {
		t.Errorf("Kind/Actor: got %q/%q", d.Kind, d.Actor)
	}
	if d.VerdictOutcome != "block" || d.FailureCategory != "content" {
		t.Errorf("VerdictOutcome/FailureCategory: got %q/%q", d.VerdictOutcome, d.FailureCategory)
	}
	if d.JudgeSessionID != "judge-7f3a" {
		t.Errorf("JudgeSessionID: got %q, want judge-7f3a", d.JudgeSessionID)
	}
	if d.Backend != "" {
		t.Errorf("Backend: got %q, want empty when a judge session exists", d.Backend)
	}
	for _, want := range []string{"security", "t1", "block", "hardcoded token in config"} {
		if !strings.Contains(d.Note, want) {
			t.Errorf("Note %q missing %q", d.Note, want)
		}
	}
}

func TestRecordGuardrailVerdict_TextOnlyBackendAndIssueFallback(t *testing.T) {
	m, prd := newBL416Manager(t)
	v := GuardrailVerdict{
		Guardrail: "rules",
		Outcome:   "warn",
		Issues:    []string{"missing changelog entry"},
		Backend:   "ollama",
	}

	if err := m.recordGuardrailVerdict(prd, "t1", v); err != nil {
		t.Fatalf("recordGuardrailVerdict: %v", err)
	}
	d := prd.Decisions[0]
	if d.Backend != "ollama" {
		t.Errorf("Backend: got %q, want ollama for a text-only verdict", d.Backend)
	}
	if !strings.Contains(d.Note, "missing changelog entry") {
		t.Errorf("Note %q does not fall back to the first issue", d.Note)
	}
}

func TestBlockedReasonFor(t *testing.T) {
	withSession := GuardrailVerdict{Guardrail: "security", Outcome: "block", Summary: "hardcoded token", JudgeSessionID: "judge-7f3a"}
	if got, want := blockedReasonFor(withSession), "guardrail security returned block (judge-7f3a) - hardcoded token"; got != want {
		t.Errorf("with session: got %q, want %q", got, want)
	}

	textOnly := GuardrailVerdict{Guardrail: "security", Outcome: "block", Summary: "hardcoded token", Backend: "ollama"}
	if got, want := blockedReasonFor(textOnly), "guardrail security returned block (ollama) - hardcoded token"; got != want {
		t.Errorf("no session: got %q, want %q", got, want)
	}
}
