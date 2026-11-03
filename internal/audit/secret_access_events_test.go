// Audit tests for the LLM API key secret-access event (AGENT.md Audit
// Logging Rule): the action name, its CEF signature, a JSON-lines
// round-trip asserting valid JSON output, and a CEF format test asserting
// header pipe-escaping, extension equals/newline-escaping and the correct
// (signatureID, name, severity) triple — with hostile header and extension
// values, since bad escapes let an attacker inject synthetic CEF events.

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

var secretAccessEvents = []struct {
	outcome string
}{
	{SecretAccessOk},
	{SecretAccessMissing},
	{SecretAccessRefused},
}

func TestSecretAccessEvents_NamesAndCEF(t *testing.T) {
	id, name, sev := cefSignature(ActionLLMSecretAccess)
	if ActionLLMSecretAccess != "llm_secret_access" {
		t.Errorf("action constant = %q, want %q", ActionLLMSecretAccess, "llm_secret_access")
	}
	if id != 65 || name != "LLMSecretAccess" || sev != 5 {
		t.Errorf("cefSignature(%q) = %d %q %d, want 65 %q 5", ActionLLMSecretAccess, id, name, sev, "LLMSecretAccess")
	}
	// The signature ID must not collide with its neighbours.
	for _, prior := range []string{
		ActionUIPromptRefused,
		ActionUIPromptNotCleared,
		ActionUIPromptDismissed,
		ActionLaneFillMode,
	} {
		if pid, _, _ := cefSignature(prior); pid == 65 {
			t.Errorf("signature id 65 is reused by %q", prior)
		}
	}
}

func TestSecretAccessEvents_Outcomes(t *testing.T) {
	want := map[string]bool{SecretAccessOk: true, SecretAccessMissing: true, SecretAccessRefused: true}
	if len(secretAccessEvents) != 3 {
		t.Fatalf("want 3 outcomes, got %d", len(secretAccessEvents))
	}
	for _, ev := range secretAccessEvents {
		if !want[ev.outcome] {
			t.Errorf("unexpected outcome %q", ev.outcome)
		}
	}
}

func TestSecretAccessEvents_JSONLines(t *testing.T) {
	for _, ev := range secretAccessEvents {
		t.Run(ev.outcome, func(t *testing.T) {
			dir := t.TempDir()
			l, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			in, err := SecretAccessEvent("claude-main", "ANTHROPIC_API_KEY", ev.outcome,
				time.Now().UTC().Truncate(time.Millisecond))
			if err != nil {
				t.Fatal(err)
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
			if out.Action != ActionLLMSecretAccess || out.Actor != "daemon" ||
				out.Subject != "claude-main" || !out.Timestamp.Equal(in.Timestamp) {
				t.Fatalf("round-trip mismatch: %+v", out)
			}
			// Exactly the three permitted detail keys — no secret value,
			// no literal key, nothing else.
			if len(out.Details) != 3 {
				t.Fatalf("want exactly 3 details, got %d: %v", len(out.Details), out.Details)
			}
			if out.Details["llm_name"] != "claude-main" ||
				out.Details["secret_name"] != "ANTHROPIC_API_KEY" ||
				out.Details["outcome"] != ev.outcome {
				t.Fatalf("details lost: %v", out.Details)
			}
		})
	}
}

func TestSecretAccessEvents_CEFEscaping(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	for _, ev := range secretAccessEvents {
		t.Run(ev.outcome, func(t *testing.T) {
			e, err := SecretAccessEvent("claude\\|main", "ANTHROPIC=KEY\nname2", ev.outcome, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			e.Actor = `da|emon=x`
			line := FormatCEFLine(e)
			wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\1|65|LLMSecretAccess|5|`
			if !strings.HasPrefix(line, wantPrefix) {
				t.Fatalf("line = %q, want header prefix %q", line, wantPrefix)
			}
			if strings.ContainsAny(line, "\n\r") {
				t.Fatalf("raw newline survived into CEF line: %q", line)
			}
			if !strings.Contains(line, `suser=da|emon\=x`) {
				t.Errorf("actor extension not escaped: %q", line)
			}
			if !strings.Contains(line, "deviceCustomString1Label=action deviceCustomString1=llm_secret_access") {
				t.Errorf("action extension missing: %q", line)
			}
			// Details sort as llm_name, outcome, secret_name: the only
			// custom slots available are cs3, cs4, cs5; cs6 carries the
			// subject.
			if !strings.Contains(line, `deviceCustomString3=claude\\|main`) {
				t.Errorf("llm_name extension not escaped: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString4Label=outcome deviceCustomString4=`+ev.outcome) {
				t.Errorf("outcome extension missing: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString5Label=secret_name deviceCustomString5=ANTHROPIC\=KEY\nname2`) {
				t.Errorf("secret_name extension not escaped: %q", line)
			}
			if !strings.Contains(line, `deviceCustomString6Label=subject deviceCustomString6=claude\\|main`) {
				t.Errorf("subject extension not escaped: %q", line)
			}
		})
	}
	// A secret-looking value passed where a name belongs stays name-shaped:
	// the constructor takes nothing but names, so the key/value never
	// reaches the entry.
	e, err := SecretAccessEvent("sk-live-DO-NOT-LEAK", "ANTHROPIC_API_KEY", SecretAccessOk, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if e.Details["secret_name"] != "ANTHROPIC_API_KEY" || e.Details["llm_name"] != "sk-live-DO-NOT-LEAK" {
		t.Fatalf("names moved between fields: %v", e.Details)
	}
	if json, _ := json.Marshal(e); strings.Contains(string(json), `"value":"`) ||
		strings.Contains(string(json), `"secret":"`) {
		t.Errorf("no secret-value detail key may be present: %s", json)
	}
}

func TestSecretAccessEvent_RejectsBadInput(t *testing.T) {
	at := time.Now()
	for _, c := range []struct {
		desc     string
		llm, sec string
		outcome  string
	}{
		{"empty llm name", "", "ANTHROPIC_API_KEY", SecretAccessOk},
		{"empty secret name", "claude-main", "", SecretAccessOk},
		{"empty outcome", "claude-main", "ANTHROPIC_API_KEY", ""},
		{"case-wrong outcome", "claude-main", "ANTHROPIC_API_KEY", "OK"},
		{"unknown outcome", "claude-main", "ANTHROPIC_API_KEY", "leaked"},
	} {
		if _, err := SecretAccessEvent(c.llm, c.sec, c.outcome, at); err == nil {
			t.Errorf("%s: want error, got entry", c.desc)
		}
	}
	// A zero timestamp is tolerated and filled in.
	if _, err := SecretAccessEvent("claude-main", "ANTHROPIC_API_KEY", SecretAccessOk, time.Time{}); err != nil {
		t.Errorf("zero timestamp should be replaced, got %v", err)
	}
}

func TestSecretAccessSink(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink := NewSecretAccessSink(l)
	sink("claude-main", "ANTHROPIC_API_KEY", "ok")
	// A malformed report is dropped loudly, never fatal.
	sink("", "ANTHROPIC_API_KEY", "ok")
	_ = l.Close()

	got, err := l.Read(QueryFilter{Action: ActionLLMSecretAccess})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry (malformed report dropped), got %d", len(got))
	}
	if got[0].Details["outcome"] != "ok" || got[0].Subject != "claude-main" ||
		got[0].Details["llm_name"] != "claude-main" || got[0].Details["secret_name"] != "ANTHROPIC_API_KEY" {
		t.Fatalf("sink entry mismatch: %+v", got[0])
	}

	// A nil log sink is a silent no-op — a missing audit log must never be
	// an error path in the dispatcher.
	nilSink := SecretAccessReporter(NewSecretAccessSink(nil))
	nilSink("claude-main", "ANTHROPIC_API_KEY", "ok")

	// The reporter type is a plain three-string function, so the inference
	// package can carry one (or a plain func of the same shape) without
	// importing internal/audit.
	var plain = func(llmName, secretName, outcome string) {}
	var r SecretAccessReporter = plain
	if r == nil {
		t.Fatal("plain func does not satisfy SecretAccessReporter")
	}
	r("a", "b", "ok")
}
