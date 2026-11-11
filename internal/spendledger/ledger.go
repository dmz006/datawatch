package spendledger

import (
	"encoding/json"
	"errors"
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
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries[key(e)] = e
	return l.save()
}

// Entries returns all retained entries ordered by start time.
func (l *Ledger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sorted()
}

// Prune drops entries that ended (or started, if still open) before now-Retention
// and advances the horizon.
func (l *Ledger) Prune(now time.Time) error {
	cutoff := now.Add(-Retention)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.entries {
		t := e.EndedAt
		if t.IsZero() {
			t = e.StartedAt
		}
		if t.Before(cutoff) {
			delete(l.entries, k)
		}
	}
	if cutoff.After(l.horizon) {
		l.horizon = cutoff
	}
	return l.save()
}

// Horizon returns the oldest time the ledger covers; spend before it is unknown.
func (l *Ledger) Horizon() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.horizon
}

func (l *Ledger) sorted() []Entry {
	out := make([]Entry, 0, len(l.entries))
	for _, e := range l.entries {
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

func (l *Ledger) save() error {
	b, err := json.MarshalIndent(fileFormat{Horizon: l.horizon, Entries: l.sorted()}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}
