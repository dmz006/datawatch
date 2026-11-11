// Shared config-write audit sink.
//
// Every behavior classified as a configuration write passes through
// exactly one audit point and yields exactly one ConfigAuditRecord per
// call, for allowed and (by default) denied attempts alike:
//
//   - MCP: a tool handler middleware (internal/mcp.RegisterConfigWriteAudit)
//     wraps every tool named in IsConfigWriteTool below, so config_set,
//     detection-config-set, dns-channel-config-set and every other
//     config-write tool emit one record with surface "mcp".
//   - REST: the PUT handlers behind fedCap(CapConfigWrite) — /api/config
//     and /api/cost/rates — record one record with surface "rest".
//   - Comm channel: the router's configure command records one record
//     with surface "comm" (the loopback REST call it makes is admin, so
//     the REST audit point skips it and exactly one record exists).
//
// A record names time, surface, tool/route, the changed key(s), the
// value (redacted for secret-like keys), the caller session ID (or peer
// name), whether the write was allowed, and the denial reason.
//
// The sink writes to stderr (always, as a visible log of operator
// actions) + appends a JSON line to the file named by
// mcp.self_config_audit_path. A nil *ConfigAuditor is a safe no-op for
// Record.

package federation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// ConfigAuditRecord is one line of the audit file / stderr sink.
type ConfigAuditRecord struct {
	Time    string            `json:"time"`
	Surface string            `json:"surface"` // mcp | rest | comm
	Tool    string            `json:"tool"`    // MCP tool name or "PUT /route"
	Keys    []string          `json:"keys,omitempty"`
	Values  map[string]string `json:"values,omitempty"` // redacted for secret-like keys
	Caller  string            `json:"caller,omitempty"` // session ID or peer name
	Allowed bool              `json:"allowed"`
	Reason  string            `json:"reason,omitempty"`
}

// ConfigAuditor is the process-wide shared config-write audit sink. All
// three surfaces (MCP middleware, REST PUT handlers, comm configure)
// record through the same instance so every config write — MCP tool
// call, REST PUT, comm "configure key=value" — lands as exactly one
// JSON line. Construct once with NewConfigAuditor; a nil *ConfigAuditor
// is a safe no-op for Record.
type ConfigAuditor struct {
	mu     sync.Mutex
	path   string
	denied bool // also record denied attempts (mcp.self_config_audit_denied)
	cef    bool // also mirror every record as a CEF line to <path>.cef
}

// NewConfigAuditor creates a sink writing to stderr (always) +, when
// path is non-empty, appending JSON lines to that file (dirs 0700, file
// 0600). recordDenied controls whether DENIED attempts are recorded at
// all — default true (read through MCPConfig.SelfConfigAuditDenied): an
// attempt is an attempt.
func NewConfigAuditor(path string, recordDenied bool) *ConfigAuditor {
	return &ConfigAuditor{path: path, denied: recordDenied}
}

// EnableCEFMirror turns on the CEF (ArcSight Common Event Format)
// mirror: from then on, every record that lands in the JSONL file also
// appends one CEF:0 line to <path>.cef for SIEM forwarding (AGENT.md's
// Audit Logging Rule — every audit-style event is emittable in both
// JSON-lines and CEF). The JSONL file stays the single source of truth
// for the read-back surfaces (GET /api/audit/self-config, the
// self_config_audit MCP tool, the CLI). A nil receiver or a sink with no
// file path is a no-op. Call before the first Record.
func (a *ConfigAuditor) EnableCEFMirror() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cef = true
}

// configAuditCEFEntry maps one config-write record onto the shared
// audit.Entry shape so the CEF line uses the same escaping, signature
// table and header the operator audit log uses (audit.FormatCEFLine).
func configAuditCEFEntry(rec ConfigAuditRecord) audit.Entry {
	ts, err := time.Parse(time.RFC3339, rec.Time)
	if err != nil {
		ts = time.Now().UTC()
	}
	details := map[string]any{
		"surface": rec.Surface,
		"allowed": strconv.FormatBool(rec.Allowed),
		"keys":    strings.Join(rec.Keys, ","),
	}
	if rec.Reason != "" {
		details["reason"] = rec.Reason
	}
	return audit.Entry{
		Timestamp: ts,
		Actor:     rec.Caller,
		Action:    audit.ActionConfigWrite,
		Subject:   rec.Tool,
		Details:   details,
	}
}

// Path returns the JSONL file the sink appends to ("" when the sink was
// created without one). Exposed so read-only views — the
// GET /api/audit/self-config handler and the self_config_audit MCP tool
// — can report and read the same file the audit point writes to, rather
// than re-deriving the default path independently.
func (a *ConfigAuditor) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// Record emits one config-write attempt. It never fails the call: audit
// is a record, not a blocking dependency — a file I/O error is logged to
// stderr and dropped. Denied attempts are dropped without being written
// when the sink was created with recordDenied=false.
func (a *ConfigAuditor) Record(rec ConfigAuditRecord) {
	if a == nil {
		return
	}
	if rec.Time == "" {
		rec.Time = time.Now().UTC().Format(time.RFC3339)
	}
	if !rec.Allowed && !a.denied {
		return
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[config-audit] marshal: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[config-audit] %s\n", raw)
	if a.path == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	dir := filepath.Dir(a.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			fmt.Fprintf(os.Stderr, "[config-audit] mkdir: %v\n", err)
			return
		}
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[config-audit] open: %v\n", err)
		return
	}
	defer f.Close() //nolint:errcheck
	_, _ = f.Write(append(raw, '\n'))
	if a.cef {
		line := audit.FormatCEFLine(configAuditCEFEntry(rec))
		cf, err := os.OpenFile(a.path+".cef", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[config-audit] cef open: %v\n", err)
			return
		}
		defer cf.Close() //nolint:errcheck
		_, _ = cf.Write([]byte(line + "\n"))
	}
}

// secretKeyHints — a key containing any of these (or a value shaped like
// a bearer credential) is written to the audit sink as <redacted>.
var secretKeyHints = []string{
	"token", "secret", "password", "passwd", "credential",
	"cookie", "bearer", "private_key", "privatekey",
	"api_key", "apikey", "client_secret",
}

// RedactConfigValue returns the audit-safe form of value: the literal
// "<redacted>" when the key (or the value's shape) looks like a secret,
// value otherwise.
func RedactConfigValue(key, value string) string {
	k := strings.ToLower(key)
	for _, h := range secretKeyHints {
		if strings.Contains(k, h) {
			return "<redacted>"
		}
	}
	v := strings.TrimSpace(value)
	// A handful of credential shapes are redacted by value even when the
	// key name does not hint at it.
	if strings.HasPrefix(v, "eyJ") && strings.Contains(v, ".") { // JWT
		return "<redacted>"
	}
	for _, p := range []string{"ghp_", "gho_", "ghs_", "sk-", "xoxb-", "AKIA"} {
		if strings.HasPrefix(strings.ToLower(v), p) {
			return "<redacted>"
		}
	}
	return value
}

// ExtractConfigPatch pulls the changed key(s) and values out of a
// config-write body. For the top-level object shape (REST /api/config
// and /api/cost/rates PUT bodies, or an MCP config-write tool argument
// map) it returns the sorted changed keys with redacted values, so a
// write named in the record is always a key that was actually written:
// dot-path keys (config_set's flat map) and the individual sub-keys of
// one-level objects are both expanded (a "/api/cost/rates" body of
// {"rates":{"claude":{...},"openai":{...}}} yields rates.claude and
// rates.openai, not a lumped "rates"); nested objects beyond that depth
// are kept as their redacted compact-JSON value. Any unrecognized shape
// (array, scalar, non-object) returns nil — the call still gets its
// audit record (surface, tool/route, caller, allowed, reason), just
// without key detail.
func ExtractConfigPatch(raw []byte) (keys []string, values map[string]string) {
	var patch map[string]any
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, nil
	}
	values = make(map[string]string)
	for k, v := range patch {
		for _, dk := range expandPatchKey(values, k, v) {
			keys = append(keys, dk)
		}
	}
	sort.Strings(keys)
	return keys, values
}

// expandPatchKey adds one top-level patch entry to values and returns the
// key name(s) it covers. A JSON object value (e.g. the "rates" map in a
// cost-rates PUT) is expanded into its direct entries so the audit record
// names the individual keys actually written; everything else is a single
// dot-path key. Values are redacted for secret-like keys (and credential
// shapes) either way.
func expandPatchKey(values map[string]string, k string, v any) []string {
	if obj, ok := v.(map[string]any); ok && len(obj) > 0 {
		children := make([]string, 0, len(obj))
		for child, cv := range obj {
			name := k + "." + child
			values[name] = RedactConfigValue(name, stringifyPatchValue(cv))
			children = append(children, name)
		}
		return children
	}
	values[k] = RedactConfigValue(k, stringifyPatchValue(v))
	return []string{k}
}

// stringifyPatchValue renders a JSON patch value for the audit sink:
// strings verbatim, everything else as compact JSON. The result passes
// through the caller's RedactConfigValue before it is stored.
func stringifyPatchValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(raw)
}
