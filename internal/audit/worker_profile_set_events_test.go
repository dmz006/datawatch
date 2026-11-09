// Audit tests for the Automaton worker-profile event (AGENT.md Audit Logging
// Rule): a JSON-lines round-trip and a CEF test with the signature triple and
// pipe, '=', backslash and newline in header and extension values.

package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutomatonWorkerProfileSetAudit_JSONLinesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "operator",
		Action:    ActionAutomatonWorkerProfileSet,
		Subject:   "automaton:66910002",
		Details: map[string]any{
			"worker_profile": "digest",
		},
	}
	if err := l.Write(in); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()

	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 1 {
		t.Fatalf("want 1 JSON line, got %d: %q", len(lines), lines)
	}
	var out Entry
	if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
		t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
	}
	if out.Action != ActionAutomatonWorkerProfileSet || out.Actor != in.Actor ||
		out.Subject != in.Subject || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Details["worker_profile"] != "digest" {
		t.Fatalf("details lost: %+v", out.Details)
	}
}

func TestAutomatonWorkerProfileSetAudit_CEFSignatureAndEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })

	line := FormatCEFLine(Entry{
		Timestamp: time.Now(),
		Actor:     "auto|nomous=x",
		Action:    ActionAutomatonWorkerProfileSet,
		Details: map[string]any{
			"worker_profile": "a=b|c\nCEF:0|evil\\z",
		},
	})
	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|84|AutonomousWorkerProfileSet|2|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	if !strings.Contains(line, `suser=auto|nomous\=x`) {
		t.Errorf("actor extension not escaped: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString1="+ActionAutomatonWorkerProfileSet) {
		t.Errorf("action extension missing: %q", line)
	}
	// The worker_profile detail is the only detail; the CEF mapper places it in
	// cs3, after the action in cs1.
	if !strings.Contains(line, `deviceCustomString3Label=worker_profile deviceCustomString3=a\=b|c\nCEF:0|evil\\z`) {
		t.Errorf("worker_profile not escaped in cs3: %q", line)
	}
}
