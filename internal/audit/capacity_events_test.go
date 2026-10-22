// Audit tests for the capacity and budget events (AGENT.md Audit Logging
// Rule): a JSON-lines round-trip and a CEF test for each action, with the
// subject, and pipe, '=', backslash and newline in header and extension values.

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

var capacityEvents = []struct {
	action string
	sigID  string
	name   string
	sev    string
}{
	{ActionCapacityWait, "37", "AutonomousCapacityWait", "3"},
	{ActionCapacityAdmitted, "38", "AutonomousCapacityAdmitted", "2"},
	{ActionBudgetWindowRollover, "39", "AutonomousBudgetWindowRollover", "2"},
	// Existing events gain a subject only.
	{ActionUsageBackoffSet, "31", "AutonomousUsageBackoffSet", "4"},
	{ActionBudgetCeilingWait, "35", "AutonomousBudgetCeilingWait", "4"},
}

func TestCapacityAudit_JSONLinesRoundTrip(t *testing.T) {
	for _, ev := range capacityEvents {
		t.Run(ev.action, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := Entry{
				Timestamp: time.Now().UTC().Truncate(time.Millisecond),
				Actor:     "autonomous",
				Action:    ev.action,
				Subject:   "task=1|x\nnew",
				Details: map[string]any{
					"prd_id": "5dfb0001",
					"pools":  []string{"host", "node:n1"},
					"cause":  "pool_full",
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
			var raw map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &raw); err != nil || raw["subject"] != in.Subject {
				t.Fatalf("subject key missing or wrong in %q (%v)", lines[0], err)
			}
			var out Entry
			if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
				t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
			}
			if out.Action != ev.action || out.Actor != "autonomous" || out.Subject != in.Subject || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			pools, _ := out.Details["pools"].([]any)
			if out.Details["prd_id"] != "5dfb0001" || out.Details["cause"] != "pool_full" || len(pools) != 2 || pools[1] != "node:n1" {
				t.Fatalf("details lost: %+v", out.Details)
			}

			// An entry with no subject must not carry the key at all.
			b, _ := json.Marshal(Entry{Action: ev.action})
			if strings.Contains(string(b), "subject") {
				t.Fatalf("empty subject was serialized: %s", b)
			}
		})
	}
}

func TestCapacityAudit_CEFSignatureAndEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, ev := range capacityEvents {
		t.Run(ev.action, func(t *testing.T) {
			line := FormatCEFLine(Entry{
				Timestamp: time.Now(),
				Actor:     "ope|rator=x",
				Action:    ev.action,
				Subject:   "t=1|x\nCEF:0|evil\\z",
				Details: map[string]any{
					"cause":  "pool_full",
					"pools":  []string{"host", "node:n1"},
					"prd_id": "a=b|c\nd",
					"reason": "pool node:n1 full (1/1)",
				},
			})
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` + ev.sigID + `|` + ev.name + `|` + ev.sev + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString6Label=subject deviceCustomString6=t\=1|x\nCEF:0|evil\\z`) {
				t.Errorf("subject extension not escaped: %q", line)
			}
			if !strings.Contains(line, `suser=ope|rator\=x`) {
				t.Errorf("actor extension not escaped: %q", line)
			}
			// Details sort as cause, pools, prd_id → cs3, cs4, cs5; reason spills to msg.
			if !strings.Contains(line, `deviceCustomString5=a\=b|c\nd`) {
				t.Errorf("prd_id extension not escaped: %q", line)
			}
			if !strings.Contains(line, `msg=reason\=pool node:n1 full (1/1)`) {
				t.Errorf("overflow detail missing from msg: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+ev.action) {
				t.Errorf("action extension missing: %q", line)
			}
		})
	}

	// An entry without a subject emits no subject extension.
	line := FormatCEFLine(Entry{Timestamp: time.Now(), Actor: "operator", Action: ActionCapacityWait})
	if strings.Contains(line, "subject") {
		t.Errorf("subject extension emitted for an empty subject: %q", line)
	}
}
