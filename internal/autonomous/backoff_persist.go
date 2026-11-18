// Persistence for the back-off registry: every mutation is written to a JSON
// file (temp file in the same folder, then rename, mode 0600) so a daemon
// restart can reinstall unexpired flags with Load. Persisting only touches
// that one file; it never signals or kills any session.
package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// backoffFile is the on-disk shape.
type backoffFile struct {
	Entries []Entry `json:"entries"`
}

// SetPersistPath turns persistence on (or off, with an empty path). It does
// not read the file; call Load for that, and Save to write the current state.
func (b *Backoff) SetPersistPath(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.persistPath = path
}

// NewBackoffPersisted returns an empty registry that persists to path. Call
// Load to restore saved flags.
func NewBackoffPersisted(path string) *Backoff {
	b := NewBackoff()
	b.persistPath = path
	return b
}

// Save writes all current entries to the persist path. It is a no-op without
// a path.
func (b *Backoff) Save() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saveLocked()
}

// SaveErr returns the error of the most recent write (nil when it succeeded),
// so a caller can log failures of the automatic saves.
func (b *Backoff) SaveErr() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saveErr
}

// saveLocked writes the file; b.mu must be held. The error is also kept in
// saveErr because mutators have no error return.
func (b *Backoff) saveLocked() error {
	if b.persistPath == "" {
		return nil
	}
	b.saveErr = writeBackoffFile(b.persistPath, b.entries)
	return b.saveErr
}

func writeBackoffFile(path string, entries map[string]Entry) error {
	list := make([]Entry, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Backend < list[j].Backend })
	data, err := json.MarshalIndent(backoffFile{Entries: list}, "", "  ")
	if err != nil {
		return fmt.Errorf("backoff persist: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("backoff persist: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".backoff-*.tmp")
	if err != nil {
		return fmt.Errorf("backoff persist: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("backoff persist: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("backoff persist: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("backoff persist: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("backoff persist: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("backoff persist: %w", err)
	}
	return nil
}

// Load reads the persist file and installs every entry whose Until is after
// now, replacing the current entries. It returns the restored entries sorted
// by backend. A missing file is not an error (zero entries); a corrupt or
// unreadable file returns zero entries and an error for the caller to log —
// Load never panics. When expired entries were dropped the file is rewritten
// so it matches memory.
func (b *Backoff) Load(now time.Time) ([]Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	restored := []Entry{}
	if b.persistPath == "" {
		return restored, nil
	}
	data, err := os.ReadFile(b.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return restored, nil
		}
		return restored, fmt.Errorf("backoff load: %w", err)
	}
	var f backoffFile
	if err := json.Unmarshal(data, &f); err != nil {
		return restored, fmt.Errorf("backoff load: corrupt file %s: %w", b.persistPath, err)
	}
	dropped := false
	for _, e := range f.Entries {
		if e.Backend == "" || !e.Until.After(now) {
			dropped = true
			continue
		}
		restored = append(restored, e)
	}
	if b.entries == nil {
		b.entries = map[string]Entry{}
	}
	for _, e := range restored {
		b.entries[e.Backend] = e
	}
	sort.Slice(restored, func(i, j int) bool { return restored[i].Backend < restored[j].Backend })
	if dropped {
		_ = b.saveLocked()
	}
	return restored, nil
}
