package usagecalc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// AutoLanesLogName is the file the auto-lanes decision log is saved as,
	// beside the usage series file.
	AutoLanesLogName = "autolanes.json"

	// autoLanesLogMaxEntries is how many decisions the log holds: the newest
	// ones are kept and older ones are dropped as newer decisions land.
	autoLanesLogMaxEntries = 500
)

// AutoLanesLogEntry is one auto-lane decision recorded in the log: when it
// happened, the cap it moved from and to, which way it moved, why, and the
// reading and policy state it was built from.
type AutoLanesLogEntry struct {
	At           time.Time `json:"at"`
	From         int       `json:"from"`
	To           int       `json:"to"`
	Direction    string    `json:"direction"`
	Reason       string    `json:"reason"`
	Policy       string    `json:"policy"`
	LanesRunning int       `json:"lanes_running"`
	SpecialItems int       `json:"special_items"`
	ReadingID    string    `json:"reading_id"`
}

// AutoLanesLog is the append-only record of auto-lane decisions, saved as
// autolanes.json in the same data directory the usage Store uses. Only the
// newest autoLanesLogMaxEntries entries are kept.
type AutoLanesLog struct {
	mu      sync.Mutex
	path    string
	entries []AutoLanesLogEntry
}

// OpenAutoLanesLog opens the auto-lanes decision log beside the usage series
// file (under dataDir like the usage Store). It never panics: a directory that
// cannot be created or a file that cannot be read or parsed all yield an empty
// log plus the error, so a caller can fall back to an in-memory log.
func OpenAutoLanesLog(dataDir string) (*AutoLanesLog, error) {
	l := &AutoLanesLog{}
	if dataDir == "" {
		return l, errors.New("usagecalc auto-lanes log: data directory is required")
	}
	dir := filepath.Join(dataDir, storeDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return l, fmt.Errorf("usagecalc auto-lanes log: mkdir %s: %w", dir, err)
	}
	l.path = filepath.Join(dir, AutoLanesLogName)
	raw, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return l, nil
		}
		return l, fmt.Errorf("usagecalc auto-lanes log: read %s: %w", l.path, err)
	}
	if len(raw) == 0 {
		return l, nil
	}
	var entries []AutoLanesLogEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return l, fmt.Errorf("usagecalc auto-lanes log: parse %s: %w", l.path, err)
	}
	if len(entries) > autoLanesLogMaxEntries {
		entries = entries[len(entries)-autoLanesLogMaxEntries:]
	}
	l.entries = entries
	return l, nil
}

// Append records one decision at the end of the log, keeps only the newest
// autoLanesLogMaxEntries entries, and persists the log atomically: the whole
// log is first written to a temp file in the same directory, which is then
// renamed over the log file, so a reader never sees a half-written log.
func (l *AutoLanesLog) Append(e AutoLanesLogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.path == "" {
		return errors.New("usagecalc auto-lanes log: log is not open")
	}
	entries := append(append([]AutoLanesLogEntry{}, l.entries...), e)
	if len(entries) > autoLanesLogMaxEntries {
		entries = entries[len(entries)-autoLanesLogMaxEntries:]
	}
	// Write first, commit second: a failed write leaves the log's in-memory
	// state exactly as it was, so the log never loses entries it still holds.
	if err := l.writeLocked(entries); err != nil {
		return err
	}
	l.entries = entries
	return nil
}

// Entries returns a copy of the log in append order, oldest first.
func (l *AutoLanesLog) Entries() []AutoLanesLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]AutoLanesLogEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Len is the number of entries currently held.
func (l *AutoLanesLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// writeLocked persists entries; the caller must hold l.mu.
func (l *AutoLanesLog) writeLocked(entries []AutoLanesLogEntry) error {
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("usagecalc auto-lanes log: encode: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(l.path), AutoLanesLogName+".tmp-")
	if err != nil {
		return fmt.Errorf("usagecalc auto-lanes log: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // best-effort; it is gone after a successful rename
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("usagecalc auto-lanes log: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("usagecalc auto-lanes log: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("usagecalc auto-lanes log: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, l.path); err != nil {
		return fmt.Errorf("usagecalc auto-lanes log: rename into place: %w", err)
	}
	return nil
}
