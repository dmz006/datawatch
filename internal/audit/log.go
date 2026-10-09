// Package audit (BL9) — append-only operator-action log.
//
// Records actions (start, kill, send-input, configure, rollback,
// schedule, …) with timestamp + actor + session + details to a
// JSON-lines file at <dataDir>/audit.log. The file is line-oriented
// for easy tail/grep + future SIEM ingestion (matches the F10
// agent-audit format).

package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry is one line in the audit log.
type Entry struct {
	Timestamp time.Time      `json:"ts"`
	Actor     string         `json:"actor"`           // who: "operator", "channel:signal", "mcp", "agent:<id>"
	Action    string         `json:"action"`          // start, kill, send_input, rollback, configure, ...
	SessionID string         `json:"session_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// Log is the append-only audit log.
type Log struct {
	mu   sync.Mutex
	path string
	f    *os.File
	cef  *os.File // CEF mirror (nil unless EnableCEFMirror succeeded)
}

// New opens (or creates) the operator audit log file at <dir>/audit.log.
func New(dir string) (*Log, error) {
	if dir == "" {
		return nil, fmt.Errorf("audit: data dir required")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return NewAt(filepath.Join(dir, "audit.log"))
}

// NewAt opens (or creates) a log file at the exact given path (GH#201 —
// used for the separate HTTP access / WS lifecycle log, access.log,
// alongside the operator audit log New opens).
func NewAt(path string) (*Log, error) {
	if path == "" {
		return nil, fmt.Errorf("audit: path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Log{path: path, f: f}, nil
}

// EnableCEFMirror (AGENT.md's Audit Logging Rule) opens <path>.cef and
// starts appending a CEF-formatted line alongside every JSON-lines
// Write, for operators forwarding to a SIEM. The JSON-lines file stays
// the sole source Read/Prune operate on — CEF is an additive mirror,
// not a format switch, since Read can't parse CEF lines back into
// Entry. Safe to call at most once per Log; a second call is a no-op.
func (l *Log) EnableCEFMirror() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cef != nil {
		return nil
	}
	f, err := os.OpenFile(l.path+".cef", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	l.cef = f
	return nil
}

// Close flushes + closes the underlying file(s).
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var err error
	if l.f != nil {
		err = l.f.Close()
		l.f = nil
	}
	if l.cef != nil {
		if cerr := l.cef.Close(); err == nil {
			err = cerr
		}
		l.cef = nil
	}
	return err
}

// Write appends one entry. Caller-supplied timestamp wins; zero gets
// time.Now(). Also appends a CEF line to the mirror file when
// EnableCEFMirror has been called.
func (l *Log) Write(e Entry) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return fmt.Errorf("audit: log closed")
	}
	if _, err := l.f.Write(append(line, '\n')); err != nil {
		return err
	}
	if l.cef != nil {
		if _, err := io.WriteString(l.cef, FormatCEFLine(e)+"\n"); err != nil {
			// CEF mirror is best-effort (SIEM forwarding, not the source
			// of truth) — don't fail the real write over it.
			fmt.Fprintf(os.Stderr, "[audit] CEF mirror write failed for %s.cef: %v\n", l.path, err)
		}
	}
	return nil
}

// Prune (GH#201) rewrites the log file keeping only entries at or after
// cutoff, dropping older ones. Returns the number of entries removed.
// Safe to call periodically on a live log — the file is reopened in place
// after rewriting so subsequent Write calls keep working.
func (l *Log) Prune(cutoff time.Time) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, fmt.Errorf("audit: log closed")
	}

	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var kept []string
	removed := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			kept = append(kept, line) // keep unparseable lines rather than silently drop data
			continue
		}
		if e.Timestamp.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed == 0 {
		return 0, nil
	}

	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	if err := l.f.Close(); err != nil {
		return 0, err
	}
	// #nosec G703 -- l.path is set once at New/NewAt construction from a
	// daemon-internal, config-derived path, never from request input.
	if err := os.WriteFile(l.path, []byte(out), 0644); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return 0, err
	}
	l.f = f
	return removed, nil
}

// QueryFilter scopes a Read call.
type QueryFilter struct {
	Since     time.Time // entries >= Since
	Until     time.Time // entries < Until (zero = no cap)
	Actor     string    // exact match (empty = any)
	Action    string    // exact match (empty = any)
	SessionID string    // exact match (empty = any)
	Limit     int       // most-recent first; 0 = unlimited
}

// Read scans the log file and returns entries matching filter, newest
// first. The scan is O(file-size) — fine up to a few hundred MB.
func (l *Log) Read(filter QueryFilter) ([]Entry, error) {
	l.mu.Lock()
	path := l.path
	l.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var matched []Entry
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip malformed lines silently
		}
		if !filter.Since.IsZero() && e.Timestamp.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && !e.Timestamp.Before(filter.Until) {
			continue
		}
		if filter.Actor != "" && e.Actor != filter.Actor {
			continue
		}
		if filter.Action != "" && e.Action != filter.Action {
			continue
		}
		if filter.SessionID != "" && e.SessionID != filter.SessionID {
			continue
		}
		matched = append(matched, e)
	}
	// Reverse for newest-first.
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}
	return matched, nil
}
