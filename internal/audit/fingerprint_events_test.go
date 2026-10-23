// Audit tests for the fingerprint-match decision events (AGENT.md Audit
// Logging Rule): a JSON-lines round-trip and a CEF escape test for each
// emitter, with pipe, '=', backslash and newline in the header AND
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

var fingerprintEvents = []struct {
	action string
	sigID  string
	name   string
	sev    string
}{
	{ActionFingerprintMatchConfirmed, "39", "FingerprintMatchConfirmed", "4"},
	{ActionFingerprintMatchRejected, "40", "FingerprintMatchRejected", "2"},
}

func TestFingerprintEvents_Names(t *testing.T) {
	want := map[string]string{
		ActionFingerprintMatchConfirmed: "fingerprint_match_confirmed",
		ActionFingerprintMatchRejected:  "fingerprint_match_rejected",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("action constant = %q, want %q", got, w)
		}
	}
}

func TestFingerprintEvents_JSONLinesRoundTrip(t *testing.T) {
	for _, ev := range fingerprintEvents {
		t.Run(ev.action, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in := Entry{
				Timestamp: time.Now().UTC().Truncate(time.Millisecond),
				Actor:     "operator",
				Action:    ev.action,
				Details: map[string]any{
					"kind":  "compute|node=x\na",
					"local": "gpu\\1",
					"peer":  "peer-2",
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
			var out Entry
			if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
				t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
			}
			if out.Action != ev.action || out.Actor != "operator" || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			if out.Details["kind"] != "compute|node=x\na" || out.Details["local"] != "gpu\\1" || out.Details["peer"] != "peer-2" {
				t.Fatalf("details lost: %+v", out.Details)
			}
		})
	}
}

func TestFingerprintEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, ev := range fingerprintEvents {
		t.Run(ev.action, func(t *testing.T) {
			line := FormatCEFLine(Entry{
				Timestamp: time.Now(),
				Actor:     "ope|rator=x",
				Action:    ev.action,
				Details: map[string]any{
					"kind":  "compute_node",
					"local": "gpu=a|b\nnew",
					"peer":  "peer\\x",
				},
			})
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` + ev.sigID + `|` + ev.name + `|` + ev.sev + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			// Details sort as kind, local, peer → cs3, cs4, cs5.
			if !strings.Contains(line, "deviceCustomString3Label=kind deviceCustomString3=compute_node") {
				t.Errorf("kind extension missing: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString4=gpu\=a|b\nnew`) {
				t.Errorf("local extension not escaped: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString5=peer\\x`) {
				t.Errorf("peer extension not escaped: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1="+ev.action) {
				t.Errorf("action extension missing: %q", line)
			}
		})
	}
}
