// AGENT.md's Audit Logging Rule: every new audit-event-emitting path
// needs (a) a JSON-lines round-trip test (log_test.go already has
// these) and (b) a CEF format test asserting header pipe-escaping,
// extension equals/newline-escaping, and the correct (signatureID,
// name, severity) triple.

package audit

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestCEF_HeaderEscaping(t *testing.T) {
	// Unit-test the escape function directly — a naive split on '|'
	// can't distinguish a real field separator from an escaped '\|'
	// inside a value, which is exactly the property being tested.
	got := cefHeaderEscape(`1|2\3`)
	want := `1\|2\\3`
	if got != want {
		t.Fatalf("cefHeaderEscape(%q) = %q, want %q", `1|2\3`, got, want)
	}

	SetCEFVersionFn(func() string { return `1|2\3` })
	defer SetCEFVersionFn(func() string { return "v?" })
	line := FormatCEFLine(Entry{Action: "start", Actor: "operator"})
	wantPrefix := `CEF:0|datawatch|datawatch|` + want + `|20|SessionStart|3|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want prefix %q", line, wantPrefix)
	}
}

func TestCEF_ExtensionEscaping(t *testing.T) {
	e := Entry{
		Action: "configure",
		Actor:  "operator",
		Details: map[string]any{
			"path": `a=b\c` + "\n" + "d\re",
		},
	}
	line := FormatCEFLine(e)
	// request= is the mapped key for Details["path"]; its value must have
	// '=' and '\' backslash-escaped and \n/\r rendered as literal \n \r.
	if !strings.Contains(line, `request=a\=b\\c\nd\re`) {
		t.Errorf("extension value not correctly escaped, line=%q", line)
	}
}

func TestCEF_SignatureSeverityTriples(t *testing.T) {
	cases := []struct {
		action   string
		sigID    int
		name     string
		severity int
	}{
		{"http_access", 10, "HTTPAccess", 2},
		{"auth_failure", 11, "AuthFailure", 7},
		{"ws_connect", 12, "WSConnect", 2},
		{"ws_disconnect", 13, "WSDisconnect", 2},
		{"start", 20, "SessionStart", 3},
		{"kill", 21, "SessionKill", 4},
		{"send_input", 22, "SessionSendInput", 2},
		{"configure", 23, "ConfigChange", 5},
		{"rollback", 24, "ConfigRollback", 6},
		{"schedule", 25, "ScheduleChange", 3},
		{"some_unmapped_future_action", 0, "AuditEvent", 3},
	}
	for _, c := range cases {
		sigID, name, severity := cefSignature(c.action)
		if sigID != c.sigID || name != c.name || severity != c.severity {
			t.Errorf("cefSignature(%q) = (%d,%q,%d), want (%d,%q,%d)",
				c.action, sigID, name, severity, c.sigID, c.name, c.severity)
		}
		line := FormatCEFLine(Entry{Action: c.action, Actor: "x"})
		wantMid := "|" + c.name + "|" + itoa(c.severity) + "|"
		if !strings.Contains(line, wantMid) {
			t.Errorf("FormatCEFLine(%q) missing %q in header: %q", c.action, wantMid, line)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestCEF_EnableMirror_WritesAlongsideJSON(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	if err := l.EnableCEFMirror(); err != nil {
		t.Fatalf("EnableCEFMirror: %v", err)
	}
	ts := time.Now()
	if err := l.Write(Entry{Timestamp: ts, Actor: "operator", Action: "start", SessionID: "aa"}); err != nil {
		t.Fatal(err)
	}

	// JSON-lines file: still readable via Read (CEF never touches it).
	entries, err := l.Read(QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "start" {
		t.Fatalf("Read after CEF mirror enabled = %+v, want 1 'start' entry", entries)
	}

	// CEF mirror file exists and holds a well-formed CEF line.
	raw, err := os.ReadFile(dir + "/audit.log.cef")
	if err != nil {
		t.Fatalf("read .cef mirror: %v", err)
	}
	cefData := string(raw)
	if !strings.HasPrefix(cefData, "CEF:0|datawatch|datawatch|") {
		t.Errorf(".cef mirror doesn't start with a CEF header: %q", cefData)
	}
	if !strings.Contains(cefData, "SessionStart") {
		t.Errorf(".cef mirror missing mapped signature name: %q", cefData)
	}
}
