// Audit tests for the Automation budget events (AGENT.md Audit Logging Rule):
// names and CEF signatures, a JSON-lines round-trip and a CEF escape test, with
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

var automationBudgetEvents = []struct {
	action string
	want   string
	sigID  int
	name   string
	sev    int
}{
	{ActionAutomationBudgetBreach, "automation_budget_breach", 37, "AutomationBudgetBreach", 5},
	{ActionAutomationBudgetResumed, "automation_budget_resumed", 38, "AutomationBudgetResumed", 2},
}

func automationBudgetDetails() map[string]any {
	return map[string]any{
		"automation_id": "a|1=x",
		"scope":         "automation",
		"spent_usd":     12.5,
		"ceiling_usd":   10.0,
		"window":        "day",
		"action":        "paused",
		"reset_at":      "2026-10-23T00:00:00Z",
		"reset_known":   true,
		"actor":         "automation",
		"reason":        "budget reached\nCEF:0|evil",
	}
}

func TestAutomationBudgetEvents_NamesAndCEF(t *testing.T) {
	seen := map[int]string{}
	for _, c := range automationBudgetEvents {
		if c.action != c.want {
			t.Errorf("action constant = %q, want %q", c.action, c.want)
		}
		id, name, sev := cefSignature(c.action)
		if id != c.sigID || name != c.name || sev != c.sev {
			t.Errorf("cefSignature(%q) = %d %q %d, want %d %q %d", c.action, id, name, sev, c.sigID, c.name, c.sev)
		}
		if other, dup := seen[id]; dup {
			t.Errorf("signature id %d used by %q and %q", id, other, c.action)
		}
		seen[id] = c.action
	}
	// Neither id may collide with the earlier budget events.
	for _, prior := range []string{ActionBudgetCeilingWait, ActionBudgetWindowResumed} {
		if id, _, _ := cefSignature(prior); seen[id] != "" {
			t.Errorf("signature id %d of %q is reused", id, prior)
		}
	}
}

func TestAutomationBudgetEvents_JSONLines(t *testing.T) {
	for _, ev := range automationBudgetEvents {
		t.Run(ev.action, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := Entry{
				Timestamp: time.Now().UTC().Truncate(time.Millisecond),
				Actor:     "automation",
				Action:    ev.action,
				Details:   automationBudgetDetails(),
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
			if out.Action != ev.action || out.Actor != "automation" || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			if out.Details["automation_id"] != "a|1=x" || out.Details["reason"] != "budget reached\nCEF:0|evil" ||
				out.Details["spent_usd"] != 12.5 || out.Details["reset_known"] != true || out.Details["reset_at"] != "2026-10-23T00:00:00Z" {
				t.Fatalf("details lost: %+v", out.Details)
			}
		})
	}
}

func TestAutomationBudgetEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, ev := range automationBudgetEvents {
		t.Run(ev.action, func(t *testing.T) {
			e := Entry{
				Timestamp: time.Now(),
				Actor:     "au|to=x",
				Action:    ev.action,
				Details:   automationBudgetDetails(),
			}
			line := FormatCEFLine(e)
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` + itoa(ev.sigID) + `|` + ev.name + `|` + itoa(ev.sev) + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, `suser=au|to\=x`) {
				t.Errorf("actor extension not escaped: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+ev.action) {
				t.Errorf("action extension missing: %q", line)
			}
			// Details sort as action, actor, automation_id, ...: cs3, cs4, cs5 are the
			// only custom slots the mirror has room for.
			if !strings.Contains(line, `deviceCustomString5Label=automation_id deviceCustomString5=a|1\=x`) {
				t.Errorf("automation_id extension not escaped: %q", line)
			}
		})
	}
	// The reason carries a newline and a CEF header lookalike; it must stay one
	// escaped line when it lands in an extension slot.
	got := cefExtEscape("budget reached\nCEF:0|evil=1")
	if got != `budget reached\nCEF:0|evil\=1` {
		t.Errorf("cefExtEscape = %q", got)
	}
}
