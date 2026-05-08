// BL274 Sprint 4 — translator output parser tests.

package server

import (
	"strings"
	"testing"
)

func TestExtractStepArray_Clean(t *testing.T) {
	in := `[
  {"tool":"observer_peers_list","args":{},"description":"List peers","read_only":true},
  {"tool":"observer_peer_register","args":{"name":"x","shape":"B"},"description":"Mint","read_only":false}
]`
	steps, err := extractStepArray(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Tool != "observer_peers_list" || !steps[0].ReadOnly {
		t.Errorf("step 0 wrong: %+v", steps[0])
	}
	if steps[1].Tool != "observer_peer_register" || steps[1].ReadOnly {
		t.Errorf("step 1 wrong: %+v", steps[1])
	}
}

func TestExtractStepArray_StripsCodeFence(t *testing.T) {
	in := "```json\n[{\"tool\":\"a\",\"description\":\"A\",\"read_only\":true}]\n```"
	steps, err := extractStepArray(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(steps) != 1 || steps[0].Tool != "a" {
		t.Errorf("expected 1 step a, got %+v", steps)
	}
}

func TestExtractStepArray_TolereatesProseAround(t *testing.T) {
	in := "Here is the JSON you asked for:\n\n[{\"tool\":\"x\",\"description\":\"do x\",\"read_only\":false}]\n\nLet me know!"
	steps, err := extractStepArray(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(steps) != 1 || steps[0].Tool != "x" {
		t.Errorf("expected 1 step x, got %+v", steps)
	}
}

func TestExtractStepArray_RejectsMalformed(t *testing.T) {
	if _, err := extractStepArray("not even close"); err == nil {
		t.Error("expected error on garbage input")
	}
}

func TestExtractStepArray_DropsToollessEntries(t *testing.T) {
	in := `[{"description":"missing tool","read_only":true},{"tool":"valid","description":"v","read_only":true}]`
	steps, _ := extractStepArray(in)
	if len(steps) != 1 || steps[0].Tool != "valid" {
		t.Errorf("expected 1 valid step, got %+v", steps)
	}
}

func TestNewDocsTranslator_NilWhenUnconfigured(t *testing.T) {
	tr := NewDocsTranslator(nil)
	if tr != nil {
		t.Error("expected nil for nil cfg")
	}
}

func TestTranslatorPromptShape(t *testing.T) {
	// Ensure the prompt template references all three placeholders so we
	// don't silently drop howto context if someone edits the constant.
	if !strings.Contains(translatorUserPromptTemplate, "%s") {
		t.Error("template lost its placeholders")
	}
	if !strings.Contains(translatorSystemPrompt, "JSON array") {
		t.Error("system prompt lost the JSON-array directive")
	}
}
