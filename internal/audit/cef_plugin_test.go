// AGENT.md's Audit Logging Rule for the plugin audit actions: every
// action needs (a) a JSON-lines round-trip test and (b) a CEF format
// test asserting header pipe-escaping, extension equals/newline
// escaping, and the correct (signatureID, name, severity) triple.
//
// pluginAuditCases covers all fourteen plugin actions cefSignature maps:
// the four service-health actions and the ten managed-service
// lifecycle, widget and service-token actions. The four cases 70-73
// were shipped by the earlier emit task and are pinned here at their
// shipped severities — the spec's keep-existing-cases rule governs
// them (see the plugin_service_token_rejected case for the one stale
// severity line in the spec). Every case carries a
// "poison" detail value holding the CEF special characters (=, \, |,
// newline) so the escaping is exercised per action, not once globally.

package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pluginAuditCase struct {
	action   string
	sigID    int
	name     string
	severity int
	actor    string
	subject  string
	details  map[string]any
	poison   string // one of the detail values, carrying CEF special characters
}

var pluginAuditCases = []pluginAuditCase{
	{
		action:   "plugin_service_down",
		sigID:    70,
		name:     "PluginServiceDown",
		severity: 4,
		actor:    "daemon",
		subject:  "ollama-mcp",
		details: map[string]any{
			"state":                "raised",
			"consecutive_failures": 3,
			"threshold":            "3 = 3|3 \\raise\n",
		},
		poison: "3 = 3|3 \\raise\n",
	},
	{
		// SHIPPED SEVERITY — do not "fix" 71 to medium without a release
		// note. The task spec's severity sentence lists this pre-existing
		// action as medium, but the spec also hard-constrains this task
		// to keep every existing cefSignature case unchanged, so 71
		// keeps the high severity 5 shipped by the earlier emit task
		// (a 401/403 token rejection is the most security-relevant of
		// the four conditions, hence one band above its medium
		// neighbours). This case pins that shipped truth and is where
		// the spec's stale "medium" line conflicts with its own
		// don't-touch rule.
		action:   "plugin_service_token_rejected",
		sigID:    71,
		name:     "PluginServiceTokenRejected",
		severity: 5,
		actor:    "daemon",
		subject:  "ollama-mcp",
		details: map[string]any{
			"state":       "raised\n401 = probe\\|x",
			"status_code": 401,
			"source":      "widget_pull",
		},
		poison: "raised\n401 = probe\\|x",
	},
	{
		action:   "plugin_service_enrichment_backlog",
		sigID:    72,
		name:     "PluginServiceBacklog",
		severity: 4,
		actor:    "daemon",
		subject:  "imap-mcp",
		details: map[string]any{
			"streak": "rise = 4 \\|per\npull",
			"pulls":  2,
		},
		poison: "rise = 4 \\|per\npull",
	},
	{
		action:   "plugin_service_model_node_unreachable",
		sigID:    73,
		name:     "PluginModelNodeUnreachable",
		severity: 4,
		actor:    "daemon",
		subject:  "ollama-mcp",
		details: map[string]any{
			"state": "raised",
			"model": "node = 192.0.2.5|9\n \\x",
		},
		poison: "node = 192.0.2.5|9\n \\x",
	},
	{
		action:   "plugin_service_start",
		sigID:    74,
		name:     "PluginServiceStart",
		severity: 2,
		actor:    "daemon",
		subject:  "weather-mcp",
		details: map[string]any{
			"pid":     1234,
			"attempt": "0 = first\\|start\n",
		},
		poison: "0 = first\\|start\n",
	},
	{
		action:   "plugin_service_stop",
		sigID:    75,
		name:     "PluginServiceStop",
		severity: 2,
		actor:    "operator",
		subject:  "weather-mcp",
		details: map[string]any{
			"reason":    "operator = stop\\|now\nconfirmed",
			"exit_code": 0,
		},
		poison: "operator = stop\\|now\nconfirmed",
	},
	{
		action:   "plugin_service_restart",
		sigID:    76,
		name:     "PluginServiceRestart",
		severity: 4,
		actor:    "daemon",
		subject:  "weather-mcp",
		details: map[string]any{
			"reason":  "unhealthy = 3\\failing|probes\nthen",
			"attempt": 2,
		},
		poison: "unhealthy = 3\\failing|probes\nthen",
	},
	{
		action:   "plugin_service_health_fail",
		sigID:    77,
		name:     "PluginServiceHealthFail",
		severity: 4,
		actor:    "daemon",
		subject:  "weather-mcp",
		details: map[string]any{
			"reason":               "timeout = 3s \\probe|X\n",
			"consecutive_failures": 2,
			"threshold":            3,
		},
		poison: "timeout = 3s \\probe|X\n",
	},
	{
		action:   "plugin_widget_registered",
		sigID:    78,
		name:     "PluginWidgetRegistered",
		severity: 2,
		actor:    "channel:signal",
		subject:  "imap-mcp",
		details: map[string]any{
			"widget_id": "unread = w|1\n\\2",
			"mode":      "push",
		},
		poison: "unread = w|1\n\\2",
	},
	{
		action:   "plugin_widget_push",
		sigID:    79,
		name:     "PluginWidgetPush",
		severity: 2,
		actor:    "plugin:imap-mcp",
		subject:  "imap-mcp",
		details: map[string]any{
			"widget_id": "unread",
			"value":     "42 = unread\\|msgs\nnew",
		},
		poison: "42 = unread\\|msgs\nnew",
	},
	{
		action:   "plugin_widget_token_issue",
		sigID:    80,
		name:     "PluginWidgetTokenIssue",
		severity: 2,
		actor:    "operator",
		subject:  "imap-mcp",
		details: map[string]any{
			"plugin":  "imap-mcp",
			"rotated": "first = issue\\|token\n",
		},
		poison: "first = issue\\|token\n",
	},
	{
		action:   "plugin_token_rotate",
		sigID:    81,
		name:     "PluginTokenRotate",
		severity: 2,
		actor:    "operator",
		subject:  "plugin-secret:imap-mcp",
		details: map[string]any{
			"plugin": "imap-mcp",
			"note":   "rotated = ok\\|v2\n",
		},
		poison: "rotated = ok\\|v2\n",
	},
	{
		action:   "plugin_token_remove",
		sigID:    82,
		name:     "PluginTokenRemove",
		severity: 2,
		actor:    "operator",
		subject:  "plugin-secret:imap-mcp",
		details: map[string]any{
			"plugin": "imap-mcp",
			"reason": "revoked = manual\\|x\nby-operator",
		},
		poison: "revoked = manual\\|x\nby-operator",
	},
	{
		action:   "plugin_widget_pull_failed",
		sigID:    83,
		name:     "PluginWidgetPullFailed",
		severity: 3,
		actor:    "daemon",
		subject:  "imap-mcp",
		details: map[string]any{
			"widget_id": "unread = 404\\|gone\n",
			"error":     "http 502",
		},
		poison: "unread = 404\\|gone\n",
	},
}

func (c pluginAuditCase) entry() Entry {
	return Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     c.actor,
		Action:    c.action,
		Subject:   c.subject,
		Details:   c.details,
	}
}

func TestPluginAudit_SignatureTriples(t *testing.T) {
	for _, c := range pluginAuditCases {
		sigID, name, severity := cefSignature(c.action)
		if sigID != c.sigID || name != c.name || severity != c.severity {
			t.Errorf("cefSignature(%q) = (%d, %q, %d), want (%d, %q, %d)",
				c.action, sigID, name, severity, c.sigID, c.name, c.severity)
		}
	}
	// The fourteen plugin signatures must be mutually distinct so a CI
	// pipeline can tell the actions apart.
	seen := map[int]string{}
	for _, c := range pluginAuditCases {
		seen[c.sigID] = c.action
	}
	if len(seen) != len(pluginAuditCases) {
		t.Errorf("plugin signature ids collide: %v", seen)
	}
}

// AGENT.md's Audit Logging Rule (a): the JSON-lines form must be valid
// JSON and round-trip losslessly — including values that carry the CEF
// special characters, which are perfectly ordinary JSON.
func TestPluginAudit_JSONLinesRoundTrip(t *testing.T) {
	for _, c := range pluginAuditCases {
		t.Run(c.action, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := c.entry()
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
			if out.Action != c.action || out.Actor != c.actor || out.Subject != c.subject ||
				!out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			// Normalise the input details through JSON as well so
			// numeric types compare like the values read back from the file.
			if b, _ := json.Marshal(in.Details); b != nil {
				var wantD map[string]any
				if err := json.Unmarshal(b, &wantD); err == nil {
					in.Details = wantD
				}
			}
			for k, want := range in.Details {
				if got := out.Details[k]; got != want {
					t.Errorf("Details[%q] = %v (%T), want %v (%T)", k, got, got, want, want)
				}
			}
			if len(out.Details) != len(in.Details) {
				t.Errorf("detail keys lost: got %+v, want %+v", out.Details, in.Details)
			}
			// The poison value must have survived intact — the whole
			// point of JSON-lines being the queryable primary format.
			if got, ok := firstStringValue(out.Details, c.poison); !ok || got != c.poison {
				t.Errorf("poison detail not round-tripped: %q", c.poison)
			}
		})
	}
}

// firstStringValue reports whether any detail value (string) equals s.
func firstStringValue(m map[string]any, s string) (string, bool) {
	for _, v := range m {
		if sv, ok := v.(string); ok && sv == s {
			return sv, true
		}
	}
	return "", false
}

// AGENT.md's Audit Logging Rule (b): the CEF form must be a single line
// whose header escapes '|' and '\', whose extension escapes '=', '\',
// '\n' and '\r', and whose (signatureID, name, severity) header triple
// is the exact registered one — for every action.
func TestPluginAudit_CEF(t *testing.T) {
	// A version string carrying '|' and '\' proves the header escape
	// per action, not just once.
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })

	for _, c := range pluginAuditCases {
		t.Run(c.action, func(t *testing.T) {
			line := FormatCEFLine(c.entry())

			// Header: exactly the registered signature triple, with the
			// version's '|' and '\' escaped per the CEF header rules.
			wantPrefix := fmt.Sprintf("CEF:0|datawatch|datawatch|9\\|0\\\\1|%d|%s|%d|",
				c.sigID, c.name, c.severity)
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want prefix %q", line, wantPrefix)
			}

			// Extension escapes: the poison value (='s, backslashes, a
			// pipe and a real newline) must appear escaped — the
			// escaper itself is pinned to literals in cef_test.go.
			if !strings.Contains(line, cefExtEscape(c.poison)) {
				t.Errorf("poison detail not extension-escaped: %q", line)
			}
			// No raw newline may survive into the CEF line — a raw one
			// is a second, attacker-placed event from the SIEM's view.
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}

			// The action and subject extensions must carry the raw
			// (extension-escaped) values.
			if !strings.Contains(line, "deviceCustomString1="+c.action) {
				t.Errorf("action extension missing: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString6="+cefExtEscape(c.subject)) {
				t.Errorf("subject extension missing: %q", line)
			}
		})
	}
}

// TestPluginAudit_CEFExplicitEscapeLiterals pins two of the poison
// values as fully literal escaped fragments (independent of the
// cefExtEscape helper used by FormatCEFLine) so a regression in the
// escaper can't silently re-justify itself.
func TestPluginAudit_CEFExplicitEscapeLiterals(t *testing.T) {
	SetCEFVersionFn(func() string { return "v9.99.0" })
	defer SetCEFVersionFn(func() string { return "v?" })

	// "first = issue\\|token\n" (a real backslash and a real newline in
	// the value) must render as: = becomes \=, the backslash becomes \\,
	// the pipe stays literal (extensions allow it), the newline becomes
	// a literal backslash-n.
	line := FormatCEFLine(Entry{
		Timestamp: time.Now(),
		Actor:     "operator",
		Action:    "plugin_widget_token_issue",
		Subject:   "imap-mcp",
		Details: map[string]any{
			"plugin":  "imap-mcp",
			"rotated": "first = issue\\|token\n",
		},
	})
	if !strings.HasPrefix(line, `CEF:0|datawatch|datawatch|v9.99.0|80|PluginWidgetTokenIssue|2|`) {
		t.Fatalf("line = %q, want plugin_widget_token_issue header", line)
	}
	// Details sort as plugin, rotated → cs3, cs4.
	if !strings.Contains(line, `deviceCustomString3Label=plugin deviceCustomString3=imap-mcp`) {
		t.Errorf("plugin extension missing: %q", line)
	}
	if !strings.Contains(line, `deviceCustomString4Label=rotated deviceCustomString4=first \= issue\\|token\n`) {
		t.Errorf("rotated extension not correctly escaped: %q", line)
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
}
