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
}

// New opens (or creates) the audit log file.
func New(dir string) (*Log, error) {
	if dir == "" {
		return nil, fmt.Errorf("audit: data dir required")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "audit.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Log{path: path, f: f}, nil
}

// Close flushes + closes the underlying file.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Write appends one entry. Caller-supplied timestamp wins; zero gets
// time.Now().
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
	return nil
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
