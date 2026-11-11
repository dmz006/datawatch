// AGENT.md's Audit Logging Rule applied to the shared config-write audit
// sink: every audit-style event needs (a) a JSON-lines round-trip test and
// (b) a CEF format test asserting header pipe-escaping, extension
// equals/newline-escaping, and the correct (signatureID, name, severity)
// triple. The JSONL file is the source of truth the read-back surfaces
// parse; the CEF mirror (<path>.cef) is what an SIEM forwarder reads.

package federation

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigAuditor_JSONRoundTrip records one allowed and one denied
// config-write attempt and reads the JSONL file back through the same
// ConfigAuditRecord type the GET /api/audit/self-config handler, the
// self_config_audit MCP tool and the CLI use — the fields must survive
// the round trip verbatim.
func TestConfigAuditor_JSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "config-self-modify.jsonl")
	sink := NewConfigAuditor(path, true)

	allowed := ConfigAuditRecord{
		Surface: "mcp",
		Tool:    "config_set",
		Keys:    []string{"mcp.allow_self_config", "session.capabilities"},
		Values: map[string]string{
			"mcp.allow_self_config":      "false",
			"session.capabilities":       `["config:write","observers:write"]`,
			"mcp.self_config_audit_path": "<redacted>",
		},
		Caller:  "sess_abc123",
		Allowed: true,
	}
	denied := ConfigAuditRecord{
		Surface: "rest",
		Tool:    "PUT /api/config",
		Caller:  "peer:edge-1",
		Allowed: false,
		Reason:  "self-modify is disabled (mcp.allow_self_config=false)",
	}
	sink.Record(allowed)
	sink.Record(denied)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	var recs []ConfigAuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec ConfigAuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	if recs[0].Surface != "mcp" || recs[0].Tool != "config_set" || !recs[0].Allowed {
		t.Errorf("allowed record round-trip wrong: %+v", recs[0])
	}
	if len(recs[0].Keys) != 2 || recs[0].Values["session.capabilities"] == "" {
		t.Errorf("allowed record keys/values round-trip wrong: %+v", recs[0])
	}
	if recs[1].Allowed || recs[1].Reason == "" {
		t.Errorf("denied record must keep allowed=false + reason, got %+v", recs[1])
	}
	if recs[1].Caller != "peer:edge-1" {
		t.Errorf("denied record caller = %q", recs[1].Caller)
	}
}

// TestConfigAuditor_CEFSignature asserts the config_write action maps to
// a stable (signatureID, name, severity) triple in the shared CEF table.
func TestConfigAuditor_CEFSignature(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "config-self-modify.jsonl")
	sink := NewConfigAuditor(path, true)
	sink.EnableCEFMirror()
	sink.Record(ConfigAuditRecord{
		Surface: "comm",
		Tool:    "configure",
		Caller:  "channel:signal",
		Allowed: false,
		Reason:  "self-modify is disabled",
	})
	data, err := os.ReadFile(path + ".cef")
	if err != nil {
		t.Fatalf("read cef: %v", err)
	}
	line := strings.TrimRight(string(data), "\n")
	// |65|ConfigWrite|5| sits between the version and the extension fields.
	if !strings.Contains(line, "|65|ConfigWrite|5|") {
		t.Fatalf("CEF line missing |65|ConfigWrite|5|: %q", line)
	}
	if !strings.HasPrefix(line, "CEF:0|datawatch|datawatch|") {
		t.Fatalf("CEF line bad header: %q", line)
	}
}

// TestConfigAuditor_CEFEscaping feeds the CEF mirror hostile values —
// pipes, backslashes, equals signs and embedded newlines in the reason,
// keys and caller — and asserts the mirror stays one physical line per
// record with '=' and '\' backslash-escaped and newlines rendered as
// literal \n in the extension fields (header pipes backslash-escaped the
// same way the operator audit log's CEF mirror does).
func TestConfigAuditor_CEFEscaping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "config-self-modify.jsonl")
	sink := NewConfigAuditor(path, true)
	sink.EnableCEFMirror()

	rec := ConfigAuditRecord{
		Surface: "mcp",
		Tool:    "config_set",
		Keys:    []string{"a=b\\c|d"},
		Caller:  "sess|pipe\\slash",
		Allowed: false,
		Reason:  "first\nsecond\re",
	}
	sink.Record(rec)

	data, err := os.ReadFile(path + ".cef")
	if err != nil {
		t.Fatalf("read cef: %v", err)
	}
	lines := bufio.NewScanner(strings.NewReader(string(data)))
	var n int
	for lines.Scan() {
		n++
		line := lines.Text()
		if !strings.HasPrefix(line, "CEF:0|datawatch|datawatch|") {
			t.Fatalf("bad header: %q", line)
		}
		// Extension escaping: '=' -> '=3D' is NOT the scheme here — the
		// CEF spec backslash scheme is. Equals and backslashes must be
		// backslash-escaped, newlines rendered as literal \n.
		if !strings.Contains(line, `a\=b\\c|d`) { // keys value (ext escaping)
			t.Errorf("keys value not escaped, line=%q", line)
		}
		if !strings.Contains(line, `suser=sess|pipe\\slash`) { // actor (ext: pipes unescaped)
			t.Errorf("caller not escaped, line=%q", line)
		}
		if !strings.Contains(line, `first\nsecond\re`) { // reason (ext escaping)
			t.Errorf("reason not escaped, line=%q", line)
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want exactly 1 CEF line (embedded newlines must be escaped), got %d", n)
	}
}

// TestConfigAuditor_CEFOffByDefault asserts the CEF mirror is additive:
// with EnableCEFMirror never called, only the JSONL file exists.
func TestConfigAuditor_CEFOffByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "config-self-modify.jsonl")
	sink := NewConfigAuditor(path, true)
	sink.Record(ConfigAuditRecord{Surface: "mcp", Tool: "config_set", Allowed: true})
	if _, err := os.Stat(path + ".cef"); !os.IsNotExist(err) {
		t.Fatalf("cef file must not exist without EnableCEFMirror (err=%v)", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("jsonl file missing: %v", err)
	}
}
