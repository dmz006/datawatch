package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Refusal steps the ledger tracks.
const (
	RefusalStepArchive = "archive"
	RefusalStepRepair  = "repair"
	RefusalStepMerge   = "merge"
)

// refusalLedgerCap bounds the in-memory and persisted ledger; oldest entries drop first.
const refusalLedgerCap = 500

// Refusal is one remembered refusal for an Automaton at a step.
type Refusal struct {
	AutomatonID string    `json:"automaton_id"`
	Step        string    `json:"step"`
	Reason      string    `json:"reason"`
	Fingerprint string    `json:"fingerprint"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Repeats     int       `json:"repeats"`
}

type refusalKey struct{ id, step string }

// RefusalLedger remembers the latest refusal per (Automaton, step) so an
// unchanged repeat is recognised with one map lookup. Safe for concurrent use.
type RefusalLedger struct {
	mu      sync.Mutex
	path    string
	entries map[refusalKey]*Refusal
}

// NewRefusalLedger builds a ledger, loading path when it exists. An empty
// path keeps the ledger in memory.
func NewRefusalLedger(path string) *RefusalLedger {
	l := &RefusalLedger{path: path, entries: map[refusalKey]*Refusal{}}
	if path == "" {
		return l
	}
	if b, err := os.ReadFile(path); err == nil {
		var list []Refusal
		if json.Unmarshal(b, &list) == nil {
			if len(list) > refusalLedgerCap {
				sort.SliceStable(list, func(i, j int) bool { return list[i].LastSeen.After(list[j].LastSeen) })
				list = list[:refusalLedgerCap]
			}
			for i := range list {
				r := list[i]
				l.entries[refusalKey{r.AutomatonID, r.Step}] = &r
			}
		}
	}
	return l
}

// Record stores a refusal; it returns true when the refusal is new or changed
// and false for an unchanged repeat (which bumps the repeat counter).
func (l *RefusalLedger) Record(automatonID, step, reason, fingerprint string, at time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := refusalKey{automatonID, step}
	if e, ok := l.entries[k]; ok && e.Reason == reason && e.Fingerprint == fingerprint {
		e.Repeats++
		e.LastSeen = at
		l.saveLocked()
		return false
	}
	l.entries[k] = &Refusal{AutomatonID: automatonID, Step: step, Reason: reason,
		Fingerprint: fingerprint, FirstSeen: at, LastSeen: at}
	l.trimLocked()
	l.saveLocked()
	return true
}

// Suppressed reports whether the stored refusal matches reason and fingerprint.
func (l *RefusalLedger) Suppressed(automatonID, step, reason, fingerprint string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[refusalKey{automatonID, step}]
	return ok && e.Reason == reason && e.Fingerprint == fingerprint
}

// Clear drops the entry for an Automaton at a step.
func (l *RefusalLedger) Clear(automatonID, step string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.entries[refusalKey{automatonID, step}]; ok {
		delete(l.entries, refusalKey{automatonID, step})
		l.saveLocked()
	}
}

// List returns all entries, newest (last seen) first.
func (l *RefusalLedger) List() []Refusal {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sortedLocked()
}

// Prune drops entries for Automata that are no longer live.
func (l *RefusalLedger) Prune(liveIDs []string) {
	if l == nil {
		return
	}
	live := make(map[string]struct{}, len(liveIDs))
	for _, id := range liveIDs {
		live[id] = struct{}{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := false
	for k := range l.entries {
		if _, ok := live[k.id]; !ok {
			delete(l.entries, k)
			changed = true
		}
	}
	if changed {
		l.saveLocked()
	}
}

func (l *RefusalLedger) sortedLocked() []Refusal {
	out := make([]Refusal, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		if out[i].AutomatonID != out[j].AutomatonID {
			return out[i].AutomatonID < out[j].AutomatonID
		}
		return out[i].Step < out[j].Step
	})
	return out
}

func (l *RefusalLedger) trimLocked() {
	for len(l.entries) > refusalLedgerCap {
		var oldest refusalKey
		var ot time.Time
		first := true
		for k, e := range l.entries {
			if first || e.LastSeen.Before(ot) {
				oldest, ot, first = k, e.LastSeen, false
			}
		}
		delete(l.entries, oldest)
	}
}

func (l *RefusalLedger) saveLocked() {
	if l.path == "" {
		return
	}
	b, err := json.Marshal(l.sortedLocked())
	if err != nil || os.MkdirAll(filepath.Dir(l.path), 0o755) != nil {
		return
	}
	tmp := l.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, l.path)
	}
}
