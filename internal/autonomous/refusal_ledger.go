package autonomous

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// refusalLedgerLimit bounds the refusals kept in memory and on disk.
const refusalLedgerLimit = 500

// refusalLedgerFile sits beside the other autonomous stores.
const refusalLedgerFile = "refusal_ledger.json"

// RefusalEntry is one refusal in the ledger: the Automaton, the step it was
// refused on, the latest reason, when it first and last happened, and how
// often it repeated.
type RefusalEntry struct {
	Automaton string `json:"automaton"`
	Step      string `json:"step"`
	Reason    string `json:"reason"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Repeats   int    `json:"repeats"`
}

// RefusalLedger is the bounded, persisted record of refused Automaton steps.
// Each Automaton and step keeps one entry; a repeat of the same refusal just
// advances its last-seen time and repeat count, keeping the latest reason.
// Entries are kept newest first. Listing the ledger never changes it.
type RefusalLedger struct {
	mu      sync.Mutex
	path    string
	entries []RefusalEntry
	dirty   bool
}

// NewRefusalLedger opens the ledger under dir. A missing file is an empty
// ledger; a corrupt one is logged and replaced with an empty ledger, never
// fatal.
func NewRefusalLedger(dir string) *RefusalLedger {
	l := &RefusalLedger{path: filepath.Join(dir, refusalLedgerFile)}
	l.load()
	return l
}

func (l *RefusalLedger) load() {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] refusal ledger: read %s: %v", l.path, err)
		}
		return
	}
	if err := json.Unmarshal(data, &l.entries); err != nil {
		log.Printf("[autonomous] refusal ledger: %s is corrupt, starting empty: %v", l.path, err)
		l.dirty = true
		l.save()
		return
	}
	if len(l.entries) > refusalLedgerLimit {
		l.entries = l.entries[:refusalLedgerLimit]
	}
}

// Record stores one refusal for an Automaton step at at (now when at is
// zero). A repeat of the same Automaton and step folds into the existing
// entry: its last-seen time and repeat count advance and it keeps the
// latest reason; otherwise a new entry is appended, newest first.
func (l *RefusalLedger) Record(automaton, step, reason string, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	seen := at.UTC().Format(time.RFC3339)
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.entries {
		e := &l.entries[i]
		if e.Automaton == automaton && e.Step == step {
			if e.FirstSeen == "" {
				e.FirstSeen = seen
			}
			e.Reason = reason
			e.LastSeen = seen
			e.Repeats++
			l.dirty = true
			l.save()
			return
		}
	}
	l.entries = append([]RefusalEntry{{
		Automaton: automaton, Step: step, Reason: reason,
		FirstSeen: seen, LastSeen: seen, Repeats: 1,
	}}, l.entries...)
	if len(l.entries) > refusalLedgerLimit {
		l.entries = l.entries[:refusalLedgerLimit]
	}
	l.dirty = true
	l.save()
}

// List returns the recorded refusals, newest first (all of them).
func (l *RefusalLedger) List() []RefusalEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]RefusalEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// save writes the ledger when it changed. The caller holds l.mu.
func (l *RefusalLedger) save() {
	if !l.dirty {
		return
	}
	data, err := json.Marshal(l.entries)
	if err == nil {
		err = atomicWriteFile(l.path, data)
	}
	if err != nil {
		log.Printf("[autonomous] refusal ledger: write %s: %v", l.path, err)
		return
	}
	l.dirty = false
}

// RefusalLedger returns the ledger the daemon records refused Automaton
// steps in (nil when not wired).
func (m *Manager) RefusalLedger() *RefusalLedger {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refusalLedger
}

// SetRefusalLedger installs the ledger the read views list refusals from.
func (m *Manager) SetRefusalLedger(l *RefusalLedger) {
	m.mu.Lock()
	m.refusalLedger = l
	m.mu.Unlock()
}

// RefusalLedger serves the refusals rows of
// GET /api/autonomous/housekeeping-repairs.
func (a *API) RefusalLedger() *RefusalLedger { return a.M.RefusalLedger() }

// SetRefusalLedger installs the ledger the read views list refusals from.
func (a *API) SetRefusalLedger(l *RefusalLedger) { a.M.SetRefusalLedger(l) }
