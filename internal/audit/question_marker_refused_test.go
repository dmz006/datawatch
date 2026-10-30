// Audit tests for the worker question marker refusal (AGENT.md Audit Logging
// Rule): a JSON-lines round-trip and a CEF test with the signature triple and
// pipe, '=', backslash and newline in header and extension values. The entry
// carries the session id, a reason category and the line hash, never the line.

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

func TestQuestionMarkerRefusedAudit_JSONLinesRoundTrip(t *testing.T) {
	if ActionQuestionMarkerRefused != "question_marker_refused" {
		t.Fatalf("ActionQuestionMarkerRefused = %q, want question_marker_refused", ActionQuestionMarkerRefused)
	}
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Actor:     "session:ab12",
		Action:    ActionQuestionMarkerRefused,
		Subject:   "host-ab12",
		Details: map[string]any{
			"session_id":  "host-ab12",
			"reason":      "too_few_options",
			"line_sha256": "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
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
		t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
	}
	if out.Action != ActionQuestionMarkerRefused || out.Actor != in.Actor ||
		out.Subject != in.Subject || !out.Timestamp.Equal(in.Timestamp) {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	for k, v := range in.Details {
		if out.Details[k] != v {
			t.Errorf("detail %q = %v, want %v", k, out.Details[k], v)
		}
	}
}

func TestQuestionMarkerRefusedAudit_CEFSignatureAndEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })

	id, name, sev := cefSignature(ActionQuestionMarkerRefused)
	if id != 61 || name != "QuestionMarkerRefused" || sev != 4 {
		t.Fatalf("cefSignature = %d %q %d, want 61 QuestionMarkerRefused 4", id, name, sev)
	}

	line := FormatCEFLine(Entry{
		Timestamp: time.Now(),
		Actor:     "sess|ion=x",
		Action:    ActionQuestionMarkerRefused,
		Subject:   "host-ab12",
		Details: map[string]any{
			"session_id":  "a=b|c\nCEF:0|evil\\z",
			"reason":      "malformed",
			"line_sha256": "abcd",
		},
	})
	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|61|QuestionMarkerRefused|4|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Fatalf("raw newline survived into CEF line: %q", line)
	}
	if !strings.Contains(line, `suser=sess|ion\=x`) {
		t.Errorf("actor extension not escaped: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString1="+ActionQuestionMarkerRefused) {
		t.Errorf("action extension missing: %q", line)
	}
	if !strings.Contains(line, `a\=b|c\nCEF:0|evil\\z`) {
		t.Errorf("session_id not escaped in the extension: %q", line)
	}
}
