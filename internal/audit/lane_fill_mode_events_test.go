// Audit tests for the lane fill mode change event (AGENT.md Audit Logging
// Rule): a JSON-lines round-trip and a CEF escape test, with pipe, '=',
// backslash and newline in the header AND extension values.

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

func TestLaneFillModeEvent_Name(t *testing.T) {
	if ActionLaneFillMode != "lane_fill_mode" {
		t.Fatalf("action constant = %q, want lane_fill_mode", ActionLaneFillMode)
	}
}

func TestLaneFillModeEvent_JSONLinesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "lane_fill",
		Action:    ActionLaneFillMode,
		Details: map[string]any{
			"kind":     "gpu",
			"old_mode": "shadow|x\nnew",
			"new_mode": "on=1",
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
	sc := bufio.NewScanner(f)
	var lines []string
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
	if out.Action != ActionLaneFillMode || out.Actor != "lane_fill" || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Details["kind"] != "gpu" || out.Details["old_mode"] != "shadow|x\nnew" || out.Details["new_mode"] != "on=1" {
		t.Fatalf("details lost: %+v", out.Details)
	}
}

func TestLaneFillModeEvent_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	line := FormatCEFLine(Entry{
		Timestamp: time.Now(),
		Actor:     "lane|fill=x",
		Action:    ActionLaneFillMode,
		Details: map[string]any{
			"kind":     "gpu",
			"old_mode": "a=b|c\nCEF:0|evil",
			"new_mode": "on\\x",
		},
	})
	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|62|AutonomousLaneFillMode|3|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	// Details sort as kind, new_mode, old_mode → cs3, cs4, cs5.
	if !strings.Contains(line, `deviceCustomString3Label=kind deviceCustomString3=gpu`) {
		t.Errorf("kind extension missing: %q", line)
	}
	if !strings.Contains(line, `deviceCustomString4=on\\x`) {
		t.Errorf("new_mode extension not escaped: %q", line)
	}
	if !strings.Contains(line, `deviceCustomString5=a\=b|c\nCEF:0|evil`) {
		t.Errorf("old_mode extension not escaped: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString1="+ActionLaneFillMode) {
		t.Errorf("action extension missing: %q", line)
	}
}
