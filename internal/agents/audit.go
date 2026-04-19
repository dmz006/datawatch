// F10 sprint 8 (S8.4) — agent event audit trail.
//
// Every Manager mutation emits an AuditEvent. Sinks are pluggable
// via the Auditor interface; the production sink writes JSON-lines
// to ~/.datawatch/audit/agents.jsonl with daily rotation. The
// in-memory test sink is exposed for tests without disk IO.
//
// Sibling system: internal/auth.TokenBroker has its own audit
// (S5.1) — also JSON-lines. Both are jq-friendly + cheap to ship.

package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEvent is one structured event in the agent audit trail.
// Fields are deliberately string-typed where possible so the
// log is human-readable + easy to grep/jq.
type AuditEvent struct {
	At        time.Time              `json:"at"`
	Event     string                 `json:"event"` // spawn | terminate | result | bootstrap | spawn_fail | revoke | sweep
	AgentID   string                 `json:"agent_id,omitempty"`
	Project   string                 `json:"project,omitempty"`
	Cluster   string                 `json:"cluster,omitempty"`
	State     string                 `json:"state,omitempty"`
	Note      string                 `json:"note,omitempty"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

// Auditor is the sink interface. Append must be safe for concurrent
// callers; failure is best-effort logged but never returned (audit
// IO must not block the spawn path).
type Auditor interface {
	Append(ev AuditEvent)
}

// MemoryAuditor is an in-process sink (tests + temporary inspection).
type MemoryAuditor struct {
	mu     sync.Mutex
	events []AuditEvent
}

// NewMemoryAuditor returns an empty in-memory sink.
func NewMemoryAuditor() *MemoryAuditor { return &MemoryAuditor{} }

// Append stores the event in memory.
func (m *MemoryAuditor) Append(ev AuditEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
}

// All returns a snapshot of every event so far.
func (m *MemoryAuditor) All() []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AuditEvent, len(m.events))
	copy(out, m.events)
	return out
}

// Recent returns the last n events (or fewer if not enough).
func (m *MemoryAuditor) Recent(n int) []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n <= 0 || n > len(m.events) {
		n = len(m.events)
	}
	out := make([]AuditEvent, n)
	copy(out, m.events[len(m.events)-n:])
	return out
}

// AuditFormat selects the wire format for FileAuditor output.
//   FormatJSONLines (default) — one JSON object per line, jq-friendly,
//                                preferred for in-house pipelines + the
//                                datawatch web UI's audit query.
//   FormatCEF                 — ArcSight Common Event Format, a syslog-
//                                compatible single-line format every
//                                major SIEM (Splunk / QRadar / ArcSight /
//                                Sentinel) parses out of the box.
//                                Pick this when forwarding to a SIEM.
type AuditFormat int

const (
	FormatJSONLines AuditFormat = iota
	FormatCEF
)

// FileAuditor writes events to a single append-only file in the
// configured format. Daily rotation is operator's responsibility
// (logrotate / k8s log shipping) — the F10 audit volume is low
// enough not to justify inline rotation logic. 0600 perms; created
// with O_APPEND to be signal-safe across crashes.
type FileAuditor struct {
	mu     sync.Mutex
	path   string
	f      *os.File
	format AuditFormat
}

// NewFileAuditor opens (or creates) path with 0600 perms in the
// JSON-lines format (default). Parent directory created at 0700
// if missing.
func NewFileAuditor(path string) (*FileAuditor, error) {
	return NewFileAuditorWithFormat(path, FormatJSONLines)
}

// NewFileAuditorWithFormat is like NewFileAuditor but lets the
// operator pick CEF for SIEM forwarding.
func NewFileAuditorWithFormat(path string, format AuditFormat) (*FileAuditor, error) {
	if path == "" {
		return nil, errors.New("FileAuditor: path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("audit dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit file: %w", err)
	}
	return &FileAuditor{path: path, f: f, format: format}, nil
}

// Append serialises the event in the configured format and appends
// to the file. Failure is recorded to stderr but never returned —
// audit IO must not block the spawn path.
func (a *FileAuditor) Append(ev AuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return
	}
	var line string
	switch a.format {
	case FormatCEF:
		line = FormatCEFLine(ev)
	default:
		raw, _ := json.Marshal(ev)
		line = string(raw)
	}
	if _, err := io.WriteString(a.f, line+"\n"); err != nil {
		fmt.Fprintf(os.Stderr, "[agents/audit] write %s: %v\n", a.path, err)
	}
}

// Close flushes and closes the file.
func (a *FileAuditor) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return nil
	}
	err := a.f.Close()
	a.f = nil
	return err
}

// ReadEventsFilter narrows a ReadEvents call. Empty fields match all.
type ReadEventsFilter struct {
	Event   string // exact match on AuditEvent.Event
	AgentID string // exact match on AuditEvent.AgentID
	Project string // exact match on AuditEvent.Project
}

// ReadEvents (BL107) parses a JSON-lines audit file and returns the
// last `limit` events matching the supplied filter. CEF files are
// not supported (CEF is for SIEM forwarding, not querying — operators
// who need CEF should run their query through the SIEM). Limit <= 0
// returns every match; limit > 0 returns the most-recent N matches.
func ReadEvents(path string, filter ReadEventsFilter, limit int) ([]AuditEvent, error) {
	if path == "" {
		return nil, errors.New("ReadEvents: path required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audit file: %w", err)
	}
	defer f.Close()

	out := []AuditEvent{}
	dec := json.NewDecoder(f)
	for {
		var ev AuditEvent
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode audit line: %w", err)
		}
		if filter.Event != "" && ev.Event != filter.Event {
			continue
		}
		if filter.AgentID != "" && ev.AgentID != filter.AgentID {
			continue
		}
		if filter.Project != "" && ev.Project != filter.Project {
			continue
		}
		out = append(out, ev)
	}

	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// Helper for the Manager to emit an event without building the full
// struct each call. Callers pass nil-safe Auditor (pre-flight check
// happens here).
func emit(a Auditor, event, agentID, project, cluster, state, note string, extra map[string]interface{}) {
	if a == nil {
		return
	}
	a.Append(AuditEvent{
		At:      time.Now().UTC(),
		Event:   event,
		AgentID: agentID,
		Project: project,
		Cluster: cluster,
		State:   state,
		Note:    note,
		Extra:   extra,
	})
}

// FormatCEFLine renders an AuditEvent in ArcSight Common Event
// Format. The wire format is:
//
//   CEF:0|datawatch|datawatch|<version>|<sigID>|<name>|<sev>|<extension>
//
// The pipe-delimited header fields must escape `|` and `\`; the
// extension's key=value pairs must escape `=`, `\`, and newlines.
// We follow the standard escapes per the ArcSight CEF spec.
//
// SignatureID + Name + Severity mappings:
//   spawn       → 100 / "AgentSpawn"        / 3 (low)
//   spawn_fail  → 101 / "AgentSpawnFailure" / 6 (medium)
//   terminate   → 110 / "AgentTerminate"    / 3
//   result      → 120 / "AgentResult"       / 3
//   bootstrap   → 130 / "AgentBootstrap"    / 4
//   revoke      → 200 / "TokenRevoke"       / 4
//   sweep       → 210 / "TokenSweep"        / 3
//   anything else → 0 / "AgentEvent"        / 3
func FormatCEFLine(ev AuditEvent) string {
	sigID, name, severity := cefSignature(ev.Event)
	header := fmt.Sprintf("CEF:0|datawatch|datawatch|%s|%d|%s|%d|",
		cefHeaderEscape(cefVersion()),
		sigID,
		cefHeaderEscape(name),
		severity)

	// Extension: standard CEF prefers known keys (rt, src, suser,
	// dvchost, msg, etc.) over free-form. We map our fields to the
	// closest CEF equivalents + put the rest in cs1..csN labelled
	// custom strings.
	ext := []string{
		"rt=" + cefExtEscape(ev.At.UTC().Format("Jan 02 2006 15:04:05.000")),
		"deviceCustomString1Label=event",
		"deviceCustomString1=" + cefExtEscape(ev.Event),
	}
	if ev.AgentID != "" {
		ext = append(ext, "duser="+cefExtEscape(ev.AgentID))
	}
	if ev.Project != "" {
		ext = append(ext, "deviceCustomString2Label=project",
			"deviceCustomString2="+cefExtEscape(ev.Project))
	}
	if ev.Cluster != "" {
		ext = append(ext, "deviceCustomString3Label=cluster",
			"deviceCustomString3="+cefExtEscape(ev.Cluster))
	}
	if ev.State != "" {
		ext = append(ext, "deviceCustomString4Label=state",
			"deviceCustomString4="+cefExtEscape(ev.State))
	}
	if ev.Note != "" {
		ext = append(ext, "msg="+cefExtEscape(ev.Note))
	}
	for k, v := range ev.Extra {
		// Best-effort serialise; CEF doesn't carry rich types so
		// stringify everything.
		ext = append(ext, "cs6Label="+cefExtEscape(k),
			"cs6="+cefExtEscape(fmt.Sprintf("%v", v)))
	}
	return header + joinSpaces(ext)
}

// cefVersion is the datawatch version embedded in the CEF header.
// Lifted from the build-time Version constant when wiring to main;
// kept as "v?" here to avoid an import cycle.
var cefVersion = func() string { return "v?" }

// cefSignature maps our event names to (signatureID, name, severity).
func cefSignature(event string) (int, string, int) {
	switch event {
	case "spawn":
		return 100, "AgentSpawn", 3
	case "spawn_fail":
		return 101, "AgentSpawnFailure", 6
	case "terminate":
		return 110, "AgentTerminate", 3
	case "result":
		return 120, "AgentResult", 3
	case "bootstrap":
		return 130, "AgentBootstrap", 4
	case "revoke":
		return 200, "TokenRevoke", 4
	case "sweep":
		return 210, "TokenSweep", 3
	default:
		return 0, "AgentEvent", 3
	}
}

// cefHeaderEscape escapes pipes + backslashes per spec.
func cefHeaderEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '|' {
			out = append(out, '\\', c)
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

// cefExtEscape escapes equals + backslashes + newlines per spec.
func cefExtEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '=':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// joinSpaces is strings.Join with a literal space separator.
func joinSpaces(xs []string) string {
	out := ""
	for i, s := range xs {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}
