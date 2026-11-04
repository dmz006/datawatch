// Audit tests for the pre-push secret-scan events (AGENT.md Audit
// Logging Rule): the action names, their CEF signatures, a JSON-lines
// round-trip asserting valid JSON output and no secret/value fields, and
// CEF format tests asserting header pipe-escaping, extension
// equals/newline-escaping and the correct (signatureID, name, severity)
// triple — with hostile header and extension values, since bad escapes let
// an attacker inject synthetic CEF events.
//
// All tests use a fixed clock: 2026-01-02 03:04:05 UTC.

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

var secretScanTestClock = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// secretShaped assembles a secret-shaped string from fragments at runtime
// so no literal secret-looking value sits in the source or the log.
func secretShaped() string {
	return strings.Join([]string{"sk", "live", "abc123"}, "-")
}

func TestSecretScanEvent_NamesAndCEF(t *testing.T) {
	if ActionSecretScanRefused != "secret_scan_refused" {
		t.Errorf("ActionSecretScanRefused = %q, want %q", ActionSecretScanRefused, "secret_scan_refused")
	}
	if ActionSecretScanShadow != "secret_scan_shadow" {
		t.Errorf("ActionSecretScanShadow = %q, want %q", ActionSecretScanShadow, "secret_scan_shadow")
	}
	if id, name, sev := cefSignature(ActionSecretScanRefused); id != 67 || name != "SecretScanRefused" || sev != 6 {
		t.Errorf("cefSignature(%q) = %d %q %d, want 67 %q 6", ActionSecretScanRefused, id, name, sev, "SecretScanRefused")
	}
	if id, name, sev := cefSignature(ActionSecretScanShadow); id != 68 || name != "SecretScanShadow" || sev != 4 {
		t.Errorf("cefSignature(%q) = %d %q %d, want 68 %q 4", ActionSecretScanShadow, id, name, sev, "SecretScanShadow")
	}
	// Refusals (a safety event) must be at least as severe as shadow
	// reports, never lower.
	_, _, refSev := cefSignature(ActionSecretScanRefused)
	_, _, shadSev := cefSignature(ActionSecretScanShadow)
	if refSev < shadSev {
		t.Errorf("refusal severity %d below shadow severity %d", refSev, shadSev)
	}
	// The signature IDs must be distinct and not collide with any
	// existing case.
	idRefused, _, _ := cefSignature(ActionSecretScanRefused)
	idShadow, _, _ := cefSignature(ActionSecretScanShadow)
	if idRefused == idShadow {
		t.Errorf("both secret scan actions share signature id %d", idRefused)
	}
	for _, a := range []string{
		"start", "configure", "rollback", ActionLLMSecretAccess,
		ActionUIPromptRefused, ActionUIPromptNotCleared, ActionUIPromptDismissed,
		ActionLaneFillMode, ActionQuestionMarkerRefused, ActionStuckRunningBlocked,
	} {
		if id, _, _ := cefSignature(a); id == idRefused || id == idShadow {
			t.Errorf("signature id %d is reused by %q", id, a)
		}
	}
}

func TestSecretScanEvent_JSONLinesRoundTrip(t *testing.T) {
	leak := secretShaped()
	for _, action := range []string{ActionSecretScanRefused, ActionSecretScanShadow} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			sink := NewSecretScanSink(l)
			// Report redacted findings plus two hostile extra keys
			// (secret, value) carrying a secret-shaped string: the sink
			// must drop them at the boundary.
			sink(action, "sess-1", "origin", "refs/heads/main", []map[string]any{{
				"detector":    "github_token",
				"path":        "se=cr\\et\n.go",
				"line":        7,
				"fingerprint": "ab12cd34ef567890",
				"secret":      leak,
				"value":       leak,
			}})
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
			var raw map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &raw); err != nil {
				t.Fatalf("line is not valid JSON: %v (%q)", err, lines[0])
			}
			var out Entry
			if err := json.Unmarshal([]byte(lines[0]), &out); err != nil {
				t.Fatal(err)
			}
			if out.Action != action || out.Actor != "daemon" || out.Subject != "sess-1" {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			if out.Timestamp.IsZero() {
				t.Fatal("timestamp not set")
			}
			wantMode := "enforce"
			if action == ActionSecretScanShadow {
				wantMode = "shadow"
			}
			if out.Details["remote"] != "origin" || out.Details["ref"] != "refs/heads/main" ||
				out.Details["mode"] != wantMode || out.Details["finding_count"] != float64(1) {
				t.Fatalf("details lost: %v", out.Details)
			}
			findings, ok := out.Details["findings"].([]any)
			if !ok || len(findings) != 1 {
				t.Fatalf("findings lost: %#v", out.Details["findings"])
			}
			fd, ok := findings[0].(map[string]any)
			if !ok {
				t.Fatalf("finding not an object: %#v", findings[0])
			}
			if fd["detector"] != "github_token" || fd["path"] != "se=cr\\et\n.go" ||
				fd["line"] != float64(7) || fd["fingerprint"] != "ab12cd34ef567890" {
				t.Fatalf("finding fields lost: %v", fd)
			}
			// No field named secret or value anywhere in the line.
			for _, k := range collectJSONKeys(raw) {
				if k == "secret" || k == "value" {
					t.Fatalf("forbidden field name %q present: %q", k, lines[0])
				}
			}
			// ... and the secret-shaped string itself never made it in.
			if strings.Contains(lines[0], leak) {
				t.Fatalf("secret-shaped string leaked into the line: %q", lines[0])
			}
		})
	}
}

func collectJSONKeys(v any) []string {
	var keys []string
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			keys = append(keys, k)
			keys = append(keys, collectJSONKeys(vv)...)
		}
	case []any:
		for _, vv := range x {
			keys = append(keys, collectJSONKeys(vv)...)
		}
	}
	return keys
}

func TestSecretScan_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	hostileFinding := SecretScanFinding{
		Detector:    "github_token",
		Path:        "a=b\\c\nd", // '=', '\', a raw newline
		Line:        42,
		Fingerprint: "ab12cd34ef567890",
	}
	for _, action := range []string{ActionSecretScanRefused, ActionSecretScanShadow} {
		t.Run(action, func(t *testing.T) {
			e, err := SecretScanEvent(action, `sess|ion\1`, `git\@github.com`, "origin/refs/heads/ma=in",
				[]SecretScanFinding{hostileFinding}, secretScanTestClock)
			if err != nil {
				t.Fatal(err)
			}
			wantID, wantName, wantSev := 68, "SecretScanShadow", 4
			if action == ActionSecretScanRefused {
				wantID, wantName, wantSev = 67, "SecretScanRefused", 6
			}
			line := FormatCEFLine(e)
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|` + itoa(wantID) + `|` + wantName + `|` + itoa(wantSev) + `|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1Label=action deviceCustomString1="+action) {
				t.Errorf("action extension missing: %q", line)
			}
			// Subject (cs6): extension escaping — a backslash is doubled; a pipe
			// stays literal (CEF only requires escaping '=' and '\' in extensions;
			// pipe escaping is a header-only rule).
			if !strings.Contains(line, `deviceCustomString6Label=subject deviceCustomString6=sess|ion\\1`) {
				t.Errorf("subject extension not escaped: %q", line)
			}
			// Details sort as finding_count, findings, mode, ref, remote →
			// cs3, cs4, cs5, then ref/remote overflow into msg.
			if !strings.Contains(line, `deviceCustomString3Label=finding_count deviceCustomString3=1`) {
				t.Errorf("finding_count missing: %q", line)
			}
			wantFindings := `deviceCustomString4Label=findings deviceCustomString4=[map[detector:github_token fingerprint:ab12cd34ef567890 line:42 path:a\=b\\c\nd]]`
			if !strings.Contains(line, wantFindings) {
				t.Errorf("findings extension not escaped: %q (want %q)", line, wantFindings)
			}
			wantMode := "shadow"
			if action == ActionSecretScanRefused {
				wantMode = "enforce"
			}
			if !strings.Contains(line, `deviceCustomString5Label=mode deviceCustomString5=`+wantMode) {
				t.Errorf("mode missing: %q", line)
			}
			// Extension '=' escaping on the ref, '\' escaping on the
			// remote, both riding in the msg overflow key.
			if !strings.Contains(line, `msg=ref\=origin/refs/heads/ma\=in; remote\=git\\@github.com`) {
				t.Errorf("ref/remote overflow not escaped: %q", line)
			}
		})
	}
}

func TestSecretScanEvent_RejectsBadInput(t *testing.T) {
	findings := []SecretScanFinding{{Detector: "github_token", Path: "a/b.go", Line: 1, Fingerprint: "ab12cd34ef567890"}}
	for _, c := range []struct {
		desc    string
		action  string
		subject string
	}{
		{"empty subject, refused", ActionSecretScanRefused, ""},
		{"empty subject, shadow", ActionSecretScanShadow, ""},
		{"empty action", "", "sess-1"},
		{"unknown action (bare mode)", "shadow", "sess-1"},
		{"unknown action (other package)", "llm_secret_access", "sess-1"},
		{"case-wrong action", "Secret_Scan_Refused", "sess-1"},
	} {
		if _, err := SecretScanEvent(c.action, c.subject, "origin", "refs/heads/main", findings, secretScanTestClock); err == nil {
			t.Errorf("%s: want error, got entry", c.desc)
		}
	}
	// A zero timestamp is tolerated and filled in (supplied clock otherwise).
	e, err := SecretScanEvent(ActionSecretScanRefused, "sess-1", "origin", "refs/heads/main", findings, time.Time{})
	if err != nil {
		t.Errorf("zero timestamp should be replaced, got %v", err)
	} else if e.Timestamp.IsZero() {
		t.Error("zero timestamp was not filled in")
	}
	// A non-zero supplied timestamp wins, unchanged.
	e, err = SecretScanEvent(ActionSecretScanShadow, "sess-1", "origin", "refs/heads/main", findings, secretScanTestClock)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Timestamp.Equal(secretScanTestClock) {
		t.Errorf("supplied timestamp = %v, want %v", e.Timestamp, secretScanTestClock)
	}
	if e.Details["mode"] != "shadow" || e.Actor != "daemon" || e.Subject != "sess-1" {
		t.Errorf("entry shape wrong: %+v", e)
	}
}

func TestSecretScan_NoSecretLeak(t *testing.T) {
	// Build a finding whose detector and path text are harmless; a
	// secret-shaped string is assembled from fragments and stuffed into
	// the one slot (fingerprint) where a hostile caller could try to hide
	// it. The serialized JSON and CEF may only carry the allowed field
	// names, and never the assembled string under any other name.
	leak := secretShaped()
	e, err := SecretScanEvent(ActionSecretScanRefused, "sess-1", "origin", "refs/heads/main", []SecretScanFinding{{
		Detector:    "github_token",
		Path:        "src/ok/file.go",
		Line:        3,
		Fingerprint: leak,
	}}, secretScanTestClock)
	if err != nil {
		t.Fatal(err)
	}
	jsonBytes, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"ts": true, "actor": true, "action": true, "session_id": true,
		"subject": true, "details": true,
		"remote": true, "ref": true, "mode": true, "finding_count": true,
		"findings": true,
		"detector": true, "path": true, "line": true, "fingerprint": true,
	}
	var raw map[string]any
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range collectJSONKeys(raw) {
		if !allowed[k] {
			t.Errorf("unexpected JSON field %q: %s", k, jsonBytes)
		}
	}
	cef := FormatCEFLine(e)
	// CEF renders the finding value under the findings custom string only;
	// its keys are the allowed names plus CEF's own header/extension keys.
	for _, forbidden := range []string{`Label=secret`, `Label=value`, "secret=", "value="} {
		if strings.Contains(cef, forbidden) {
			t.Errorf("forbidden CEF field %q: %q", forbidden, cef)
		}
	}
	if strings.Contains(cef, "\n") || strings.Contains(cef, "\r") {
		t.Errorf("CEF line is not single-line: %q", cef)
	}
}

func TestSecretScanSink(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink := NewSecretScanSink(l)
	sink(ActionSecretScanShadow, "sess-1", "origin", "refs/heads/main", []map[string]any{{
		"detector": "github_token", "path": "a/b.go", "line": 2, "fingerprint": "ab12cd34ef567890",
	}})
	// A malformed report (empty subject) is dropped loudly, never fatal.
	sink(ActionSecretScanShadow, "", "origin", "refs/heads/main", nil)
	// A report with the wrong action is dropped too — never misclassified.
	sink("not_an_action", "sess-1", "origin", "refs/heads/main", nil)
	_ = l.Close()

	got, err := l.Read(QueryFilter{Action: "secret_scan*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry (malformed reports dropped), got %d", len(got))
	}
	if got[0].Action != ActionSecretScanShadow || got[0].Subject != "sess-1" ||
		got[0].Details["mode"] != "shadow" || got[0].Details["finding_count"] != float64(1) {
		t.Fatalf("sink entry mismatch: %+v", got[0])
	}

	// A nil log sink is a silent no-op — a missing audit log must never be
	// an error path in a manager.
	nilSink := SecretScanReporter(NewSecretScanSink(nil))
	nilSink(ActionSecretScanRefused, "sess-1", "origin", "refs/heads/main", nil)

	// The reporter type uses stdlib parameters only, so a manager can
	// carry a compatible plain func WITHOUT importing internal/audit.
	var plain = func(action, subject, remote, ref string, findings []map[string]any) {}
	var r SecretScanReporter = plain
	if r == nil {
		t.Fatal("plain func does not satisfy SecretScanReporter")
	}
}
