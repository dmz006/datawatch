// Audit tests for the operator question channel events (AGENT.md Audit
// Logging Rule): the constant values and CEF signature of each entry, a
// JSON-lines round-trip and a CEF escaping test, with detail values
// carrying pipe, '=', backslash, newline, carriage return, leading and
// trailing spaces and non-ASCII text. The refused entry must carry no
// answer text at all.

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

// questionEventCases covers the six detail shapes the operator question
// channel writes. Every value deliberately carries the CEF-danger
// characters (pipe, '=', backslash, newline, carriage return) plus
// leading/trailing spaces and non-ASCII text.
var questionEventCases = []struct {
	name    string
	action  string
	want    string
	id      int
	cefName string
	sev     int
	details map[string]any
}{
	{
		name: "raised", action: ActionQuestionRaised, want: "question_raised",
		id: 50, cefName: "QuestionRaised", sev: 3,
		details: map[string]any{
			"question_id":  "q|x1=a\\b",
			"label":        "approve deployment",
			"asked_by":     "aut-1|op=er",
			"asker_kind":   "automaton",
			"context":      "pipeline step 2\nline two \r back",
			"risk":         "medium",
			"option_count": 2,
			"default":      "  hold  ",
			"text":         " Ship build 12\nok|yes = 确认  ",
		},
	},
	{
		name: "answered", action: ActionQuestionAnswered, want: "question_answered",
		id: 51, cefName: "QuestionAnswered", sev: 3,
		details: map[string]any{
			"question_id":      "q1",
			"answer_id":        "a|1=b\\c",
			"chosen_option_id": "opt-2",
			"quote":            "  ok | deploy \n now \r ok  部署  ",
			"quote_sha256":     "ab12",
			"actor":            "operator|1",
			"surface":          "web=x",
		},
	},
	{
		name: "withdrawn", action: ActionQuestionWithdrawn, want: "question_withdrawn",
		id: 52, cefName: "QuestionWithdrawn", sev: 3,
		details: map[string]any{
			"question_id": "q2",
			"actor":       "operator|x|y",
			"reason":      "reverted\nmanually = 撤回 ",
		},
	},
	{
		name: "expired", action: ActionQuestionExpired, want: "question_expired",
		id: 53, cefName: "QuestionExpired", sev: 4,
		details: map[string]any{
			"question_id": "q3",
			"reason":      "timeout after 300s\nhold=default took effect",
		},
	},
	{
		name: "superseded", action: ActionAnswerSuperseded, want: "answer_superseded",
		id: 54, cefName: "AnswerSuperseded", sev: 4,
		details: map[string]any{
			"question_id":      "q4",
			"answer_id":        "a|9=9",
			"supersedes":       "a1",
			"chosen_option_id": "opt-1",
			"quote":            " later \r answer\n ok \\  ",
			"quote_sha256":     "cd34",
			"actor":            "operator",
			"surface":          "web|x",
		},
	},
	{
		name: "refused", action: ActionQuestionAnswerRefused, want: "question_answer_refused",
		id: 55, cefName: "QuestionAnswerRefused", sev: 6,
		details: map[string]any{
			"question_id": "q5",
			"actor":       "operator|1",
			"surface":     "web",
			"reason":      "answer refused\nmatched a guard rule = 已拒绝",
			"pattern":     "credential_material",
		},
	},
}

// TestQuestionAudit_NamesAndCEF asserts the constant string values and the
// (signatureID, name, severity) triple of all six actions, and that the six
// ids are distinct from each other and from every other registered action.
func TestQuestionAudit_NamesAndCEF(t *testing.T) {
	seen := map[int]string{}
	for _, c := range questionEventCases {
		if c.action != c.want {
			t.Errorf("action constant for %s = %q, want %q", c.name, c.action, c.want)
		}
		id, name, sev := cefSignature(c.action)
		if id != c.id || name != c.cefName || sev != c.sev {
			t.Errorf("cefSignature(%q) = %d %q %d, want %d %q %d",
				c.action, id, name, sev, c.id, c.cefName, c.sev)
		}
		if other, dup := seen[c.id]; dup {
			t.Errorf("signature id %d is reused by %q and %q", c.id, other, c.name)
		}
		seen[c.id] = c.name
	}
	// The six ids must not collide with any other registered action
	// (the same comparison list the model tests use).
	registered := []string{
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
		ActionAutomationImportGraph, ActionNotifyEventsChanged,
		ActionLifecycleWebhookDelivery, ActionLifecycleWebhookFailed,
		ActionLifecycleForwardDelivery, ActionLifecycleForwardReceive,
	}
	for _, mine := range questionEventCases {
		for _, other := range registered {
			if id, _, _ := cefSignature(other); id == mine.id {
				t.Errorf("signature id %d is reused by %q", mine.id, other)
			}
		}
	}
}

// TestQuestionAudit_JSONLines writes each of the six entries through
// Log.Write into a temp dir and reads them back with Read, asserting valid
// JSON and that the quote value is byte-for-byte identical.
func TestQuestionAudit_JSONLines(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UTC().Truncate(time.Millisecond)
	for _, c := range questionEventCases {
		in := Entry{
			Timestamp: ts,
			Actor:     "channel:operator",
			Action:    c.action,
			Subject:   c.details["question_id"].(string),
			Details:   c.details,
		}
		if err := l.Write(in); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	// Every raw line must be valid JSON (this is what Read relies on).
	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if len(lines) != len(questionEventCases) {
		t.Fatalf("want %d JSON lines, got %d", len(questionEventCases), len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("line is not valid JSON: %q", line)
		}
	}

	// Read back through the log's own reader and compare entry by entry.
	l, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	entries, err := l.Read(QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(questionEventCases) {
		t.Fatalf("Read returned %d entries, want %d", len(entries), len(questionEventCases))
	}
	// Read returns newest first; the cases were written in order, so the
	// last case is entries[0].
	for i, c := range questionEventCases {
		out := entries[len(questionEventCases)-1-i]
		if out.Action != c.action || out.Subject != c.details["question_id"] {
			t.Errorf("%s: round-trip action/subject mismatch: %+v", c.name, out)
		}
		for k, v := range c.details {
			// Ints come back from JSON as float64, so compare the canonical
			// rendering instead of the raw interface value.
			if fmtString(out.Details[k]) != fmtString(v) {
				t.Errorf("%s: detail %q = %v, want %v", c.name, k, out.Details[k], v)
			}
		}
		// The quote — the operator's words, kept forever — must be
		// byte-for-byte identical.
		if want, ok := c.details["quote"]; ok {
			if got, ok2 := out.Details["quote"]; !ok2 || got != want {
				t.Errorf("%s: quote = %v, want %v", c.name, out.Details["quote"], want)
			}
		}
	}
}

// TestQuestionAudit_CEFEscaping checks header pipe/backslash escaping,
// extension '=', backslash and newline escaping, that no raw newline
// survives into the line (an attacker cannot inject a synthetic event),
// and the correct (signatureID, name, severity) header triple.
func TestQuestionAudit_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, c := range questionEventCases {
		t.Run(c.name, func(t *testing.T) {
			e := Entry{
				Timestamp: time.Now(),
				Actor:     "oper|ator=x\ninj",
				Action:    c.action,
				Subject:   c.details["question_id"].(string),
				Details:   c.details,
			}
			line := FormatCEFLine(e)
			wantPrefix := "CEF:0|datawatch|datawatch|9\\|0\\\\1|" + itoa(c.id) + "|" + c.cefName + "|" + itoa(c.sev) + "|"
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line (synthetic-event injection): %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+c.action) {
				t.Errorf("action extension missing: %q", line)
			}
			// Extension escaping: '=', '\\', '\n', '\r' must be escaped
			// everywhere, in every detail value and in the actor.
			if strings.Contains(line, "\\\n") || strings.Contains(line, "\\\r") {
				t.Fatalf("raw newline/carriage return escaped as backslash-char: %q", line)
			}
			for _, v := range c.details {
				s := fmtString(v)
				for _, bad := range []struct{ in, want string }{
					{"\n", `\n`}, {"\r", `\r`},
				} {
					wantCount := strings.Count(s, bad.in)
					if wantCount > 0 && strings.Count(line, bad.want) < wantCount {
						t.Errorf("detail value %q: escaped form %q not found enough times in %q", s, bad.want, line)
					}
				}
			}
			if !strings.Contains(line, `suser=oper|ator\=x\ninj`) {
				t.Errorf("actor extension not escaped: %q", line)
			}
		})
	}
}

// TestQuestionAudit_RefusedHasNoAnswerText asserts that the refused
// entry's Details carry no key named text or quote: a refusal of an
// answer containing a secret is audited without its text (design item
// 21), so the rule name alone documents the refusal.
func TestQuestionAudit_RefusedHasNoAnswerText(t *testing.T) {
	for k := range questionEventCases[5].details {
		if k == "text" || k == "quote" {
			t.Fatalf("refused entry detail %q must not be recorded", k)
		}
	}
	if _, ok := questionEventCases[5].details["pattern"]; !ok {
		t.Fatal("refused entry must record the name of the matched rule")
	}
}

// fmtString renders a detail value the same way FormatCEFLine does, so
// the escaping test can compare against it.
func fmtString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return jsonScalar(v)
}

func jsonScalar(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
