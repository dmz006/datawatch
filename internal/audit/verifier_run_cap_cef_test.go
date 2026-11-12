package audit

import (
	"strings"
	"testing"
	"time"
)

// TestVerifierRunCap_CEFSignature: the audit action written when a verifier
// session is ended at its run cap has its own CEF signature (86), not the
// generic fallback.
func TestVerifierRunCap_CEFSignature(t *testing.T) {
	e := Entry{
		Timestamp: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC),
		Actor:     "autonomous",
		Action:    "autonomous.verifier_run_cap",
		Subject:   "a1b2c3d4-task",
		Details:   map[string]any{"cap_seconds": 2700},
	}
	line := FormatCEFLine(e)
	if !strings.Contains(line, "|86|AutonomousVerifierRunCap|4|") {
		t.Errorf("CEF signature missing: %q", line)
	}
	if strings.Contains(line, "|0|AuditEvent|") {
		t.Errorf("fell back to the generic signature: %q", line)
	}
}
