// Audit tests for the notify-events-changed event (AGENT.md Audit Logging
// Rule): name and CEF signature for the Automaton- and profile-scope
// entries, a JSON-lines round-trip and a CEF escaping test, with pipe, '=',
// backslash and newline in the details values.

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

// notifyEventsChangedCases covers the two detail shapes the setter writes:
// the Automaton entry (with source_after) and the profile entry (no
// source_after). The values deliberately carry the CEF-danger characters
// (pipe, '=', backslash, newline).
var notifyEventsChangedCases = []struct {
	name    string
	details map[string]any
}{
	{
		name: "automaton_scope",
		details: map[string]any{
			"scope":        "automaton",
			"prd_id":       "prd|a1=b",
			"old":          `prd.blocked\task.completed`,
			"new":          "",
			"source_after": "global",
		},
	},
	{
		name: "profile_scope",
		details: map[string]any{
			"scope":      "profile",
			"profile_id": "gp|2=2",
			"old":        "",
			"new":        "task.completed,prd.completed\nCEF:0|evil",
		},
	},
}

func TestNotifyEventsChanged_NamesAndCEF(t *testing.T) {
	if ActionNotifyEventsChanged != "autonomous_notify_events_changed" {
		t.Errorf("ActionNotifyEventsChanged = %q, want %q",
			ActionNotifyEventsChanged, "autonomous_notify_events_changed")
	}
	id, name, sev := cefSignature(ActionNotifyEventsChanged)
	if id != 45 || name != "NotifyEventsChanged" || sev != 3 {
		t.Errorf("cefSignature(%q) = %d %q %d, want 45 %q 3",
			ActionNotifyEventsChanged, id, name, sev, "NotifyEventsChanged")
	}
	// The id must not collide with any other registered action.
	for _, other := range []string{
		"http_access", "auth_failure", "ws_connect", "ws_disconnect",
		"start", "kill", "send_input", "configure", "rollback", "schedule",
		"autonomous_file_drift_detected", "config_deprecated_key",
		"automation_create", "automation_update", "automation_delete",
		ActionUsageBackoffSet, ActionUsageBackoffCleared,
		ActionUsageBackoffAutoCleared, ActionUsageBackoffPatternsRechecked,
		ActionBudgetCeilingWait, ActionBudgetWindowResumed,
		ActionAutomationBudgetBreach, ActionAutomationBudgetResumed,
		ActionCapacityWait, ActionCapacityAdmitted, ActionBudgetWindowRollover,
		ActionFingerprintMatchConfirmed, ActionFingerprintMatchRejected,
		ActionAutomationImportGraph,
	} {
		if id2, _, _ := cefSignature(other); id2 == 45 {
			t.Errorf("signature id 45 is reused by %q", other)
		}
	}
}

func TestNotifyEventsChanged_ScopeDetails(t *testing.T) {
	wantKeys := map[string][]string{
		"automaton_scope": {"scope", "prd_id", "old", "new", "source_after"},
		"profile_scope":   {"scope", "profile_id", "old", "new"},
	}
	for _, c := range notifyEventsChangedCases {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range wantKeys[c.name] {
				if _, ok := c.details[k]; !ok {
					t.Errorf("detail %q missing from %s entry", k, c.name)
				}
			}
			if c.details["scope"] == "automaton" {
				if _, ok := c.details["profile_id"]; ok {
					t.Errorf("automaton entry must not carry profile_id")
				}
			} else if _, ok := c.details["source_after"]; ok {
				t.Errorf("profile entry must not carry source_after")
			}
		})
	}
}

func TestNotifyEventsChanged_JSONLines(t *testing.T) {
	for _, c := range notifyEventsChangedCases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := Entry{
				Timestamp: time.Now().UTC().Truncate(time.Millisecond),
				Actor:     "operator",
				Action:    ActionNotifyEventsChanged,
				Details:   c.details,
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
			if out.Action != ActionNotifyEventsChanged || out.Actor != "operator" ||
				!out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			// Every detail value must survive the round-trip verbatim, including
			// the pipe/'='/backslash/newline characters.
			for k, v := range c.details {
				if out.Details[k] != v {
					t.Errorf("detail %q = %v, want %v", k, out.Details[k], v)
				}
			}
		})
	}
}

func TestNotifyEventsChanged_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, c := range notifyEventsChangedCases {
		t.Run(c.name, func(t *testing.T) {
			e := Entry{
				Timestamp: time.Now(),
				Actor:     "oper|ator=x",
				Action:    ActionNotifyEventsChanged,
				Details:   c.details,
			}
			line := FormatCEFLine(e)
			wantPrefix := "CEF:0|datawatch|datawatch|9\\|0\\\\1|" + itoa(45) + "|NotifyEventsChanged|3|"
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, `suser=oper|ator\=x`) {
				t.Errorf("actor extension not escaped: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+ActionNotifyEventsChanged) {
				t.Errorf("action extension missing: %q", line)
			}
		})
	}
	// A detail value with a CEF header lookalike must stay one escaped line.
	got := cefExtEscape("task.completed\nCEF:0|evil=1")
	if got != `task.completed\nCEF:0|evil\=1` {
		t.Errorf("cefExtEscape = %q", got)
	}
}
