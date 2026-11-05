// Audit tests for the classifier_publisher_add event emitted by
// internal/server/classifier_surfaces.go (AGENT.md Audit Logging Rule): a
// JSON-lines round-trip, a CEF format test with the signature triple, and an
// escape test with hostile characters in model, publisher and revision.

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

const classifierPublisherAddAction = "classifier_publisher_add"

func TestClassifierPublisherAdd_JSONLinesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "operator",
		Action:    classifierPublisherAddAction,
		Subject:   "qwen",
		Details: map[string]any{
			"publisher": "qwen", "actor": "chat", "confirmed": true,
			"model": "qwen3:8b", "revision": "r1",
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
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 1 {
		t.Fatalf("want 1 JSON line, got %d: %q", len(lines), lines)
	}
	var out Entry
	if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
		t.Fatalf("not valid JSON: %v (%q)", err, lines[0])
	}
	if out.Action != in.Action || out.Actor != in.Actor || out.Subject != in.Subject || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	for k, v := range in.Details {
		if out.Details[k] != v {
			t.Errorf("detail %q = %v, want %v", k, out.Details[k], v)
		}
	}
}

func TestClassifierPublisherAdd_CEFFormat(t *testing.T) {
	SetCEFVersionFn(func() string { return "v1" })
	defer SetCEFVersionFn(func() string { return "v?" })

	// Not a named signature: it takes the generic fallback triple.
	id, name, sev := cefSignature(classifierPublisherAddAction)
	if id != 0 || name != "AuditEvent" || sev != 3 {
		t.Fatalf("cefSignature = %d %q %d, want 0 AuditEvent 3", id, name, sev)
	}
	line := FormatCEFLine(Entry{
		Timestamp: time.Now(), Actor: "operator", Action: classifierPublisherAddAction,
		Subject: "qwen", Details: map[string]any{"publisher": "qwen", "actor": "chat", "confirmed": true},
	})
	if !strings.HasPrefix(line, "CEF:0|datawatch|datawatch|v1|0|AuditEvent|3|") {
		t.Fatalf("header = %q", line)
	}
	for _, want := range []string{
		"suser=operator",
		"deviceCustomString1=" + classifierPublisherAddAction,
		"deviceCustomString6=qwen",
		"deviceCustomString3Label=actor", "deviceCustomString3=chat",
		"deviceCustomString4Label=confirmed", "deviceCustomString4=true",
		"deviceCustomString5Label=publisher", "deviceCustomString5=qwen",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in %q", want, line)
		}
	}
}

func TestClassifierPublisherAdd_Escaping(t *testing.T) {
	SetCEFVersionFn(func() string { return "v1" })
	defer SetCEFVersionFn(func() string { return "v?" })

	hostile := "a=b|c\nCEF:0|evil\\z\r"
	e := Entry{
		Timestamp: time.Now().UTC(), Actor: "op|er=x", Action: classifierPublisherAddAction,
		Subject: hostile,
		Details: map[string]any{"model": hostile, "publisher": hostile, "revision": hostile},
	}
	line := FormatCEFLine(e)
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	if !strings.Contains(line, `suser=op|er\=x`) {
		t.Errorf("actor not escaped: %q", line)
	}
	esc := `a\=b|c\nCEF:0|evil\\z\r`
	for _, label := range []string{"model", "publisher", "revision", "subject"} {
		if !strings.Contains(line, "Label="+label+" ") {
			t.Errorf("label %s missing: %q", label, line)
		}
	}
	if got := strings.Count(line, esc); got != 4 {
		t.Errorf("escaped value appears %d times, want 4: %q", got, line)
	}

	// The JSON-lines form must survive the same hostile values intact.
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Write(e); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	raw, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimRight(string(raw), "\n"), "\n"); n != 0 {
		t.Fatalf("hostile newline split the JSON line (%d extra lines)", n)
	}
	var out Entry
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"model", "publisher", "revision"} {
		if out.Details[k] != hostile {
			t.Errorf("detail %s = %q, want %q", k, out.Details[k], hostile)
		}
	}
}
