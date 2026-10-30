package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// TestOverlapShadowEntryFields checks the one entry a shadow create writes:
// its action, actor, subject (the created id), and details (mode and the
// would-be warnings).
func TestOverlapShadowEntryFields(t *testing.T) {
	var got []audit.Entry
	sink := func(e audit.Entry) { got = append(got, e) }
	warnings := []OverlapWarning{{
		Kind:        warningKindAutomaton,
		ID:          "prd-old",
		Status:      "draft",
		Score:       0.75,
		SharedTerms: []string{"automata", "duplicate"},
		Reason:      "Automaton prd-old already describes this plan.",
	}}

	RecordCreateSearchShadow(sink, "operator", "prd-new", "shadow", warnings)

	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	e := got[0]
	if e.Action != "automaton_create_search" {
		t.Errorf("Action = %q, want automaton_create_search", e.Action)
	}
	if e.Actor != "operator" {
		t.Errorf("Actor = %q, want operator", e.Actor)
	}
	if e.Subject != "prd-new" {
		t.Errorf("Subject = %q, want the created id prd-new", e.Subject)
	}
	if mode, _ := e.Details["mode"].(string); mode != "shadow" {
		t.Errorf("details mode = %v, want shadow", e.Details["mode"])
	}
	ws, ok := e.Details["warnings"].([]OverlapWarning)
	if !ok {
		t.Fatalf("details warnings has type %T, want []OverlapWarning", e.Details["warnings"])
	}
	if len(ws) != 1 || ws[0].ID != "prd-old" || ws[0].Score != 0.75 {
		t.Errorf("details warnings = %+v, want the one prd-old warning", ws)
	}
}

// TestOverlapShadowNoWarningsIsEmptyList checks that a create with no
// overlaps still records the entry, with an empty warnings list rather than
// a null.
func TestOverlapShadowNoWarningsIsEmptyList(t *testing.T) {
	var got []audit.Entry
	RecordCreateSearchShadow(func(e audit.Entry) { got = append(got, e) }, "operator", "prd-new", "shadow", nil)

	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	ws, ok := got[0].Details["warnings"].([]OverlapWarning)
	if !ok || ws == nil {
		t.Fatalf("details warnings = %#v, want a non-nil empty list", got[0].Details["warnings"])
	}
	if len(ws) != 0 {
		t.Errorf("warnings = %+v, want none", ws)
	}
}

// TestOverlapShadowNilSinkDropsEntry checks that a missing audit sink is
// a no-op rather than a panic.
func TestOverlapShadowNilSinkDropsEntry(t *testing.T) {
	RecordCreateSearchShadow(nil, "operator", "prd-new", "shadow", nil)
}

// TestOverlapShadowWritesThroughAuditLog checks the entry through the real
// audit log, so the JSON line carries the warning fields in snake_case.
func TestOverlapShadowWritesThroughAuditLog(t *testing.T) {
	dir := t.TempDir()
	log, err := audit.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	defer log.Close()

	RecordCreateSearchShadow(func(e audit.Entry) { _ = log.Write(e) }, "operator", "prd-new", "shadow", []OverlapWarning{{
		Kind:   warningKindBranch,
		Branch: "automaton/abc12345",
		Score:  1,
		Reason: "An unmerged Automaton branch already carries this work (automaton/abc12345).",
	}})

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	line := strings.TrimSpace(string(data))
	for _, want := range []string{
		`"action":"automaton_create_search"`,
		`"subject":"prd-new"`,
		`"mode":"shadow"`,
		`"kind":"branch"`,
		`"branch":"automaton/abc12345"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("audit line missing %s:\n%s", want, line)
		}
	}
}
