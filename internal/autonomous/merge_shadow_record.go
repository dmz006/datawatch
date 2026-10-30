// Automaton merge-pending shadow decisions. In merge_pending_mode shadow the
// merge-pending pass records what it would have done for each finished
// Automaton branch (its state, how far ahead of main it is, and whether it
// would have alerted) without writing a record or raising an alert. The list
// is bounded and persisted beside the other autonomous stores, in the shape
// of the lane fill decision history.
package autonomous

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// mergeShadowRecordLimit bounds the shadow decision history kept in memory
// and on disk.
const mergeShadowRecordLimit = 1000

// mergeShadowRecordsFile sits beside the other autonomous stores.
const mergeShadowRecordsFile = "merge_shadow_decisions.json"

// MergeShadowDecision is one shadow decision for a finished Automaton.
type MergeShadowDecision struct {
	At           time.Time `json:"at"`
	AutomatonID  string    `json:"automaton_id"`
	Name         string    `json:"name"`
	Branch       string    `json:"branch"`
	State        string    `json:"state"`
	CommitsAhead int       `json:"commits_ahead"`
	WouldAlert   bool      `json:"would_alert"`
	RepeatCount  int       `json:"repeat_count"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// mergeShadowRecords is the bounded, persisted shadow decision history,
// newest first. A decision equal to the newest one for the same Automaton
// only advances that record's last_seen_at and repeat_count.
type mergeShadowRecords struct {
	mu      sync.Mutex
	path    string
	entries []*MergeShadowDecision
	dirty   bool
}

// newMergeShadowRecords opens the history under dir. A missing file is an
// empty history; a corrupt one is logged and replaced with an empty history,
// never fatal.
func newMergeShadowRecords(dir string) *mergeShadowRecords {
	r := &mergeShadowRecords{path: filepath.Join(dir, mergeShadowRecordsFile)}
	r.load()
	return r
}

func (r *mergeShadowRecords) load() {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] merge shadow records: read %s: %v", r.path, err)
		}
		return
	}
	var list []MergeShadowDecision
	if err := json.Unmarshal(data, &list); err != nil {
		log.Printf("[autonomous] merge shadow records: %s is corrupt, starting empty: %v", r.path, err)
		r.dirty = true
		r.save()
		return
	}
	if len(list) > mergeShadowRecordLimit {
		list = list[:mergeShadowRecordLimit]
	}
	for i := range list {
		d := list[i]
		r.entries = append(r.entries, &d)
	}
}

// observe stores one decision, or advances the newest equal one for the same
// Automaton. It reports whether a new record was appended.
func (r *mergeShadowRecords) observe(d MergeShadowDecision, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = true
	for _, e := range r.entries {
		if e.AutomatonID != d.AutomatonID {
			continue
		}
		if e.Branch == d.Branch && e.State == d.State && e.CommitsAhead == d.CommitsAhead && e.WouldAlert == d.WouldAlert {
			e.Name = d.Name
			e.LastSeenAt = now
			e.RepeatCount++
			return false
		}
		break // the newest record for this Automaton differs: start a new one
	}
	d.At, d.LastSeenAt, d.RepeatCount = now, now, 1
	r.entries = append([]*MergeShadowDecision{&d}, r.entries...)
	if len(r.entries) > mergeShadowRecordLimit {
		r.entries = r.entries[:mergeShadowRecordLimit]
	}
	return true
}

// list returns up to limit decisions, newest first (all of them when limit is
// not positive).
func (r *mergeShadowRecords) list(limit int) []MergeShadowDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.entries)
	if limit > 0 && limit < n {
		n = limit
	}
	out := make([]MergeShadowDecision, 0, n)
	for _, e := range r.entries[:n] {
		out = append(out, *e)
	}
	return out
}

// flush writes the history when it changed.
func (r *mergeShadowRecords) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.save()
}

// save writes the history when it changed. The caller holds r.mu.
func (r *mergeShadowRecords) save() {
	if !r.dirty {
		return
	}
	list := make([]MergeShadowDecision, 0, len(r.entries))
	for _, e := range r.entries {
		list = append(list, *e)
	}
	data, err := json.Marshal(list)
	if err == nil {
		err = atomicWriteFile(r.path, data)
	}
	if err != nil {
		log.Printf("[autonomous] merge shadow records: write %s: %v", r.path, err)
		return
	}
	r.dirty = false
}
