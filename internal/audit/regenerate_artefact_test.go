package audit

import (
	"strings"
	"testing"
	"time"
)

// TestRegenerateArtefact_AuditActionFormats: the regenerate_artefact action
// the generated-artefacts REST route emits has its own CEF signature (66)
// and round-trips through the JSON-lines log and the CEF mirror.
func TestRegenerateArtefact_AuditActionFormats(t *testing.T) {
	e := Entry{
		Timestamp: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC),
		Actor:     "operator",
		Action:    "regenerate_artefact",
		Subject:   "a1b2c3d4",
		Details:   map[string]any{"kind": "gate_report", "status": "written"},
	}

	// JSON-lines: written and read back unchanged.
	l, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.Write(e); err != nil {
		t.Fatal(err)
	}
	out, err := l.Read(QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Action != "regenerate_artefact" || out[0].Subject != "a1b2c3d4" {
		t.Fatalf("round trip lost the entry: %+v", out)
	}
	if out[0].Details["kind"] != "gate_report" {
		t.Errorf("kind detail lost: %+v", out[0].Details)
	}

	// CEF: a specific signature, not the generic fallback.
	line := FormatCEFLine(e)
	if !strings.Contains(line, "|66|AutomatonArtefactRegenerated|3|") {
		t.Errorf("CEF signature missing: %q", line)
	}
	if strings.Contains(line, "|0|AuditEvent|") {
		t.Errorf("fell back to the generic signature: %q", line)
	}
	if !strings.Contains(line, "suser=operator") || !strings.Contains(line, "gate_report") {
		t.Errorf("actor or kind missing from CEF line: %q", line)
	}
}
