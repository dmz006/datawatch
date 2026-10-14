// AGENT.md's Audit Logging Rule: every new audit-event-emitting path
// needs (a) a JSON-lines round-trip test (log_test.go already has
// these) and (b) a CEF format test asserting header pipe-escaping,
// extension equals/newline-escaping, and the correct (signatureID,
// name, severity) triple.

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
		{"autonomous_file_drift_detected", 26, "AutonomousFileDriftDetected", 3},
		{"config_deprecated_key", 27, "ConfigDeprecatedKey", 2},
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

// autonomous_file_drift_detected — emitted by the autonomous executor
// via Manager.SetAuditFn when a task's commit swept in pre-existing
// uncommitted files. JSON-lines round-trip of the files list.
func TestAutonomousFileDriftDetected_JSONLinesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"docs/a=b.md", "line\nbreak.go", "x|y.txt"}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "autonomous",
		Action:    "autonomous_file_drift_detected",
		Details:   map[string]any{"task_id": "t1", "prd_id": "p1", "files": files},
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
	if out.Action != in.Action || out.Actor != "autonomous" || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Details["task_id"] != "t1" || out.Details["prd_id"] != "p1" {
		t.Fatalf("ids lost: %+v", out.Details)
	}
	got, ok := out.Details["files"].([]any)
	if !ok || len(got) != len(files) {
		t.Fatalf("files lost: %#v", out.Details["files"])
	}
	for i, f := range files {
		if got[i] != f {
			t.Errorf("files[%d] = %v, want %q", i, got[i], f)
		}
	}
}

func TestAutonomousFileDriftDetected_CEF(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	line := FormatCEFLine(Entry{
		Timestamp: time.Now(),
		Actor:     "autonomous",
		Action:    "autonomous_file_drift_detected",
		Details: map[string]any{
			"task_id": "t1",
			"prd_id":  "p1",
			"files":   []string{"a=b.go", "evil\nCEF:0|x|y.go"},
		},
	})
	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|26|AutonomousFileDriftDetected|3|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want prefix %q", line, wantPrefix)
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	// Details sort as files, prd_id, task_id → cs3, cs4, cs5.
	if !strings.Contains(line, `deviceCustomString3Label=files deviceCustomString3=[a\=b.go evil\nCEF:0|x|y.go]`) {
		t.Errorf("files extension not correctly escaped: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString4=p1") || !strings.Contains(line, "deviceCustomString5=t1") {
		t.Errorf("prd_id/task_id missing: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString1=autonomous_file_drift_detected") {
		t.Errorf("action extension missing: %q", line)
	}
}
