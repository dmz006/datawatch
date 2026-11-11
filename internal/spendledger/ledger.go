package spendledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Retention is how long ledger entries are kept.
const Retention = 31 * 24 * time.Hour

// Roles an entry can be attributed to.
const (
	RoleWorker       = "worker"
	RoleDecompose    = "decompose"
	RoleRulesReview  = "rules_review"
	RoleVerification = "verification"
	RoleGuardrail    = "guardrail"
	RoleOther        = "other"
)

// Entry is one spend record.
type Entry struct {
	SessionID     string    `json:"session_id"`
	AutomatonID   string    `json:"automaton_id,omitempty"`
	TaskID        string    `json:"task_id,omitempty"`
	Role          string    `json:"role"`
	Backend       string    `json:"backend,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at,omitempty"`
	TokensIn      int64     `json:"tokens_in"`
	TokensOut     int64     `json:"tokens_out"`
	CacheRead     int64     `json:"cache_read"`
	CacheCreation int64     `json:"cache_creation"`
	ToolCalls     int64     `json:"tool_calls"`
	ActiveSeconds float64   `json:"active_seconds"`
}

type fileFormat struct {
	Horizon time.Time `json:"horizon,omitempty"`
	Entries []Entry   `json:"entries"`
}

// Ledger is a JSON-persisted spend store.
type Ledger struct {
	mu      sync.Mutex
	path    string
	now     func() time.Time
	entries map[string]Entry
	horizon time.Time
}

// Open loads (or creates) the ledger in dataDir. A nil clock uses time.Now.
func Open(dataDir string, now func() time.Time) (*Ledger, error) {
	if now == nil {
		now = time.Now
	}
	l := &Ledger{path: filepath.Join(dataDir, "spend_ledger.json"), now: now, entries: map[string]Entry{}}
	b, err := os.ReadFile(l.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		l.horizon = now()
		if err := l.save(l.entries, l.horizon); err != nil {
			return nil, err
		}
		return l, nil
	case err != nil:
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for _, e := range f.Entries {
		l.entries[key(e)] = e
	}
	l.horizon = f.Horizon
	if l.horizon.IsZero() {
		l.horizon = now()
		if err := l.save(l.entries, l.horizon); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// key identifies an entry: one per session and role.
func key(e Entry) string { return e.SessionID + "\x00" + e.Role }

// Upsert inserts or replaces the entry for its session and role, then saves.
func (l *Ledger) Upsert(e Entry) error {
	if e.SessionID == "" {
		return errors.New("spendledger: session_id required")
	}
	if e.Role == "" {
		e.Role = RoleOther
	}
	if !validRole(e.Role) {
		return fmt.Errorf("spendledger: unknown role %q", e.Role)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	next := make(map[string]Entry, len(l.entries)+1)
	for k, v := range l.entries {
		next[k] = v
	}
	next[key(e)] = e
	if err := l.save(next, l.horizon); err != nil {
		return err
	}
	l.entries = next
	return nil
}

func validRole(r string) bool {
	switch r {
	case RoleWorker, RoleDecompose, RoleRulesReview, RoleVerification, RoleGuardrail, RoleOther:
		return true
	}
	return false
}

// Entries returns all retained entries ordered by start time.
func (l *Ledger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return sortEntries(l.entries)
}

// Prune drops entries that ended (or started, if still open) before now-Retention
// and advances the horizon.
func (l *Ledger) Prune(now time.Time) error {
	cutoff := now.Add(-Retention)
	l.mu.Lock()
	defer l.mu.Unlock()
	next := make(map[string]Entry, len(l.entries))
	for k, e := range l.entries {
		t := e.EndedAt
		if t.IsZero() {
			t = e.StartedAt
		}
		if !t.Before(cutoff) {
			next[k] = e
		}
	}
	horizon := l.horizon
	if cutoff.After(horizon) {
		horizon = cutoff
	}
	if err := l.save(next, horizon); err != nil {
		return err
	}
	l.entries, l.horizon = next, horizon
	return nil
}

// Horizon returns the oldest time the ledger covers; spend before it is unknown.
func (l *Ledger) Horizon() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.horizon
}

func sortEntries(m map[string]Entry) []Entry {
	out := make([]Entry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return key(out[i]) < key(out[j])
	})
	return out
}

// save writes the given state atomically: temp file, fsync, rename, fsync dir.
func (l *Ledger) save(entries map[string]Entry, horizon time.Time) error {
	b, err := json.MarshalIndent(fileFormat{Horizon: horizon, Entries: sortEntries(entries)}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
