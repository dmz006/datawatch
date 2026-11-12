// Audit tests for the stuck-running watchdog event (AGENT.md Audit Logging
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

func TestStuckRunningAudit_JSONLinesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "autonomous.stuck-running",
		Action:    ActionStuckRunningBlocked,
		Details: map[string]any{
			"prd_id":        "66910001",
			"caller":        "autonomous.stuck-running",
			"stuck_seconds": 1805,
			"bound_seconds": 900,
			"old_status":    "running",
			"new_status":    "blocked",
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
	if out.Action != ActionStuckRunningBlocked || out.Actor != in.Actor || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Details["prd_id"] != "66910001" || out.Details["old_status"] != "running" ||
		out.Details["new_status"] != "blocked" || out.Details["stuck_seconds"] != float64(1805) ||
		out.Details["bound_seconds"] != float64(900) {
		t.Fatalf("details lost: %+v", out.Details)
	}
}

func TestStuckRunningAudit_CEFSignatureAndEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })

	line := FormatCEFLine(Entry{
		Timestamp: time.Now(),
		Actor:     "auto|nomous=x",
		Action:    ActionStuckRunningBlocked,
		Details: map[string]any{
			"prd_id":        "a=b|c\nCEF:0|evil\\z",
			"caller":        "autonomous.stuck-running",
			"stuck_seconds": 1805,
			"bound_seconds": 900,
			"old_status":    "running",
			"new_status":    "blocked",
		},
	})
	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|60|AutonomousStuckRunningBlocked|5|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	if !strings.Contains(line, `suser=auto|nomous\=x`) {
		t.Errorf("actor extension not escaped: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString1="+ActionStuckRunningBlocked) {
		t.Errorf("action extension missing: %q", line)
	}
	// Details sort as bound_seconds, caller, new_status → cs3..cs5; the rest spill to msg.
	if !strings.Contains(line, `deviceCustomString3Label=bound_seconds deviceCustomString3=900`) {
		t.Errorf("bound_seconds not in cs3: %q", line)
	}
	if !strings.Contains(line, `prd_id\=a\=b|c\nCEF:0|evil\\z`) {
		t.Errorf("prd_id not escaped in msg overflow: %q", line)
	}
}
