// Audit tests for the usage back-off events (AGENT.md Audit Logging Rule):
// a JSON-lines round-trip and a CEF escape test for each emitter, with
// pipe, '=', backslash and newline in the header AND extension values.

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

var usageBackoffEvents = []struct {
	action string
	sigID  string
	name   string
	sev    string
}{
	{ActionUsageBackoffSet, "31", "AutonomousUsageBackoffSet", "4"},
	{ActionUsageBackoffCleared, "32", "AutonomousUsageBackoffCleared", "3"},
	{ActionUsageBackoffAutoCleared, "33", "AutonomousUsageBackoffAutoCleared", "2"},
}

func TestUsageBackoffEvents_Names(t *testing.T) {
	want := map[string]string{
		ActionUsageBackoffSet:         "autonomous_usage_backoff_set",
		ActionUsageBackoffCleared:     "autonomous_usage_backoff_cleared",
		ActionUsageBackoffAutoCleared: "autonomous_usage_backoff_auto_cleared",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("action constant = %q, want %q", got, w)
		}
	}
}

func TestUsageBackoffEvents_JSONLinesRoundTrip(t *testing.T) {
	for _, ev := range usageBackoffEvents {
		t.Run(ev.action, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := Entry{
				Timestamp: time.Now().UTC().Truncate(time.Millisecond),
				Actor:     "operator",
				Action:    ev.action,
				Details: map[string]any{
					"backend": "claude=code|x\nnew",
					"until":   "2026-10-22T12:00:00Z",
					"source":  "operator",
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
			if out.Action != ev.action || out.Actor != "operator" || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			if out.Details["backend"] != "claude=code|x\nnew" || out.Details["until"] != "2026-10-22T12:00:00Z" || out.Details["source"] != "operator" {
				t.Fatalf("details lost: %+v", out.Details)
			}
		})
	}
}

func TestUsageBackoffEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, ev := range usageBackoffEvents {
		t.Run(ev.action, func(t *testing.T) {
			line := FormatCEFLine(Entry{
				Timestamp: time.Now(),
				Actor:     "ope|rator=x",
				Action:    ev.action,
				Details: map[string]any{
					"backend": "a=b|c\nCEF:0|evil",
					"until":   "2026-10-22T12:00:00Z",
					"source":  "rate=limit\\x",
				},
			})
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` + ev.sigID + `|` + ev.name + `|` + ev.sev + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			// Details sort as backend, source, until → cs3, cs4, cs5.
			if !strings.Contains(line, `deviceCustomString3Label=backend deviceCustomString3=a\=b|c\nCEF:0|evil`) {
				t.Errorf("backend extension not escaped: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString4=rate\=limit\\x`) {
				t.Errorf("source extension not escaped: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString5=2026-10-22T12:00:00Z") {
				t.Errorf("until extension missing: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+ev.action) {
				t.Errorf("action extension missing: %q", line)
			}
		})
	}
}
