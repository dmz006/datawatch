// Audit tests for the lifecycle outbound webhook events (AGENT.md Audit
// Logging Rule): names and CEF signatures, a JSON-lines round-trip and a CEF
// escape test with pipe, '=', backslash and newline in the header AND the
// extension values.

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

var lifecycleWebhookEvents = []struct {
	action string
	want   string
	sigID  int
	name   string
	sev    int
}{
	{ActionLifecycleWebhookDelivery, "autonomous_lifecycle_webhook_delivery", 45, "AutonomousLifecycleWebhookDelivery", 2},
	{ActionLifecycleWebhookFailed, "autonomous_lifecycle_webhook_failed", 46, "AutonomousLifecycleWebhookFailed", 6},
}

const lifecycleWebhookHostileError = "boom\nCEF:0|evil|x|1|2|3|9|a=b\\c"

func lifecycleWebhookDetails() map[string]any {
	return map[string]any{
		"subscription_id": "sub|1=x",
		"delivery_id":     "d|1=x",
		"attempt":         3,
		"max_attempts":    5,
		"host":            "hooks.example.net:443",
		"status_code":     502,
		"outcome":         "failed",
		"error":           lifecycleWebhookHostileError,
		"duration_ms":     int64(1234),
	}
}

func TestLifecycleWebhookEvents_NamesAndCEF(t *testing.T) {
	seen := map[int]string{}
	for _, c := range lifecycleWebhookEvents {
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
	// The final failure must rank above the plain delivery record.
	_, _, delivered := cefSignature(ActionLifecycleWebhookDelivery)
	_, _, failed := cefSignature(ActionLifecycleWebhookFailed)
	if failed <= delivered {
		t.Errorf("final failure severity %d must exceed delivery severity %d", failed, delivered)
	}
}

// TestLifecycleWebhookEvents_SignatureIDsUnique walks every action this
// package maps and asserts that no two of them share a signature ID, so a new
// action cannot silently reuse an ID.
func TestLifecycleWebhookEvents_SignatureIDsUnique(t *testing.T) {
	actions := []string{
		"http_access", "auth_failure", "ws_connect", "ws_disconnect", "start", "kill",
		"send_input", "configure", "rollback", "schedule", "autonomous_file_drift_detected",
		"config_deprecated_key", "automation_create", "automation_update", "automation_delete",
		ActionUsageBackoffSet, ActionUsageBackoffCleared, ActionUsageBackoffAutoCleared,
		ActionUsageBackoffPatternsRechecked, ActionBudgetCeilingWait, ActionBudgetWindowResumed,
		ActionAutomationBudgetBreach, ActionAutomationBudgetResumed, ActionCapacityWait,
		ActionCapacityAdmitted, ActionBudgetWindowRollover, ActionFingerprintMatchConfirmed,
		ActionFingerprintMatchRejected, ActionAutomationImportGraph,
		ActionLifecycleWebhookDelivery, ActionLifecycleWebhookFailed,
	}
	seen := map[int]string{}
	for _, a := range actions {
		id, name, _ := cefSignature(a)
		if id == 0 || name == "AuditEvent" {
			t.Errorf("action %q has no specific CEF signature", a)
			continue
		}
		if other, dup := seen[id]; dup {
			t.Errorf("signature id %d is used by both %q and %q", id, other, a)
		}
		seen[id] = a
	}
}

func TestLifecycleWebhookEvents_JSONLines(t *testing.T) {
	for _, ev := range lifecycleWebhookEvents {
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
				Details:   lifecycleWebhookDetails(),
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
			if out.Action != ev.action || out.Actor != "autonomous" || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			if out.Details["subscription_id"] != "sub|1=x" || out.Details["delivery_id"] != "d|1=x" ||
				out.Details["error"] != lifecycleWebhookHostileError || out.Details["outcome"] != "failed" ||
				out.Details["host"] != "hooks.example.net:443" || out.Details["attempt"] != float64(3) ||
				out.Details["status_code"] != float64(502) || out.Details["duration_ms"] != float64(1234) {
				t.Fatalf("details lost: %+v", out.Details)
			}
		})
	}
}

func TestLifecycleWebhookEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, ev := range lifecycleWebhookEvents {
		t.Run(ev.action, func(t *testing.T) {
			e := Entry{
				Timestamp: time.Now(),
				Actor:     "au|to=x",
				Action:    ev.action,
				Details:   lifecycleWebhookDetails(),
			}
			line := FormatCEFLine(e)

			// (b) header triple, with the version's pipe and backslash escaped.
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` + itoa(ev.sigID) + `|` + ev.name + `|` + itoa(ev.sev) + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			// (c) one physical line: the hostile newline must be escaped.
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, `suser=au|to\=x`) {
				t.Errorf("actor extension not escaped: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+ev.action) {
				t.Errorf("action extension missing: %q", line)
			}
			// Details sort as attempt, delivery_id, duration_ms, error, ...: cs3,
			// cs4 and cs5 carry the first three, the rest land in msg.
			if !strings.Contains(line, `deviceCustomString4Label=delivery_id deviceCustomString4=d|1\=x`) {
				t.Errorf("delivery_id extension not escaped: %q", line)
			}
			// The hostile error sits in msg with its '=', '\' and newline escaped.
			if !strings.Contains(line, `error\=boom\nCEF:0|evil|x|1|2|3|9|a\=b\\c`) {
				t.Errorf("hostile error not escaped in msg: %q", line)
			}
			// The only unescaped header-looking text is the real header.
			if got := strings.Count(line, "CEF:0|datawatch|"); got != 1 {
				t.Errorf("want exactly one real CEF header, found %d in %q", got, line)
			}
		})
	}
	if got := cefExtEscape(lifecycleWebhookHostileError); got != `boom\nCEF:0|evil|x|1|2|3|9|a\=b\\c` {
		t.Errorf("cefExtEscape = %q", got)
	}
}
