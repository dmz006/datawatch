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
	"strings"
	"sync"
	"time"
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
}

// NewConfigAuditor creates a sink writing to stderr (always) +, when
// path is non-empty, appending JSON lines to that file (dirs 0700, file
// 0600). recordDenied controls whether DENIED attempts are recorded at
// all — default true (read through MCPConfig.SelfConfigAuditDenied): an
// attempt is an attempt.
func NewConfigAuditor(path string, recordDenied bool) *ConfigAuditor {
	return &ConfigAuditor{path: path, denied: recordDenied}
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
// config-write body. For the flat dot-path map shape (REST /api/config
// and /api/cost/rates PUT bodies, or an MCP config-write tool argument
// map) it returns the sorted keys with redacted values; for any
// unrecognized shape it returns nil — the call still gets its audit
// record (surface, tool/route, caller, allowed, reason), just without
// key detail.
func ExtractConfigPatch(raw []byte) (keys []string, values map[string]string) {
	var patch map[string]any
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, nil
	}
	names := make([]string, 0, len(patch))
	for k := range patch {
		names = append(names, k)
	}
	sort.Strings(names)
	values = make(map[string]string, len(patch))
	for _, k := range names {
		keys = append(keys, k)
		values[k] = RedactConfigValue(k, stringifyPatchValue(patch[k]))
	}
	return keys, values
}

// stringifyPatchValue renders a JSON patch value for the audit sink:
// strings verbatim, everything else as compact JSON.
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
