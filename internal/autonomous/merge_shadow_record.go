// Automaton merge-pending shadow decisions. In merge_pending_mode shadow the
// merge-pending pass records what it would have done for each finished
// Automaton branch (its state, how far ahead of main it is, and whether it
// would have alerted) without writing a record or raising an alert. The list
// is bounded and persisted beside the other autonomous stores, in the shape
// of the lane fill decision history, together with two running counts that
// outlive the bound: how many decisions were made and how many of them were
// later found false (a would-alert decision whose episode ended as empty or
// needs_look, not merged).
package autonomous

import (
	"bytes"
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
	At          time.Time `json:"at"`
	AutomatonID string    `json:"automaton_id"`
	Name        string    `json:"name"`
	// AutomatonState is the Automaton's own state when the decision was made
	// (completed, archived, needs_review, blocked or failed).
	AutomatonState string `json:"automaton_state,omitempty"`
	Branch         string `json:"branch"`
	// State is the merge comparison state of the branch.
	State        string `json:"state"`
	CommitsAhead int    `json:"commits_ahead"`
	WouldAlert   bool   `json:"would_alert"`
	// FoundFalse marks a would_alert decision whose episode later ended with
	// the branch empty or needing a look rather than merged: the alert it
	// stood for would have been a false one.
	FoundFalse  bool      `json:"found_false"`
	RepeatCount int       `json:"repeat_count"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// mergeShadowFile is the persisted shape of the shadow decision history.
type mergeShadowFile struct {
	DecisionsTotal int                   `json:"decisions_total"`
	DecisionsFalse int                   `json:"decisions_false"`
	Decisions      []MergeShadowDecision `json:"decisions"`
}

// mergeShadowRecords is the bounded, persisted shadow decision history,
// newest first. A decision equal to the newest one for the same Automaton
// only advances that record's last_seen_at and repeat_count. total and
// falseCount are running counts kept beside the list and never trimmed by
// its bound.
type mergeShadowRecords struct {
	mu         sync.Mutex
	path       string
	entries    []*MergeShadowDecision
	total      int
	falseCount int
	dirty      bool
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
			log.Printf("[autonomous] Automaton merge shadow records: read %s: %v", r.path, err)
		}
		return
	}
	var file mergeShadowFile
	var err2 error
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
		// the older layout: a bare list, with no counts kept beside it
		err2 = json.Unmarshal(trimmed, &file.Decisions)
		file.DecisionsTotal = len(file.Decisions)
		for _, d := range file.Decisions {
			if d.FoundFalse {
				file.DecisionsFalse++
			}
		}
	} else {
		err2 = json.Unmarshal(data, &file)
	}
	if err2 != nil {
		log.Printf("[autonomous] Automaton merge shadow records: %s is corrupt, starting empty: %v", r.path, err2)
		r.dirty = true
		r.save()
		return
	}
	list := file.Decisions
	r.total, r.falseCount = file.DecisionsTotal, file.DecisionsFalse
	if r.total < len(list) {
		r.total = len(list)
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
// Automaton. It reports whether a new record was appended, and marks the
// history for writing only when something other than a repeat count changed.
// A new decision whose branch is empty or needs a look ends the Automaton's
// pending episode as not merged, so every would_alert decision of that
// episode is marked found false first.
func (r *mergeShadowRecords) observe(d MergeShadowDecision, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.AutomatonID != d.AutomatonID {
			continue
		}
		if e.Branch == d.Branch && e.State == d.State && e.CommitsAhead == d.CommitsAhead &&
			e.WouldAlert == d.WouldAlert && e.AutomatonState == d.AutomatonState {
			// an unchanged decision only advances the in-memory counters; they
			// reach disk with the next change, so a quiet poll writes nothing
			e.LastSeenAt = now
			e.RepeatCount++
			if e.Name != d.Name {
				e.Name = d.Name
				r.dirty = true
			}
			return false
		}
		break // the newest record for this Automaton differs: start a new one
	}
	r.dirty = true
	if d.State == MergeStateEmpty || d.State == MergeStateNeedsLook {
		r.markEpisodeFalse(d.AutomatonID)
	}
	d.At, d.LastSeenAt, d.RepeatCount, d.FoundFalse = now, now, 1, false
	r.total++
	r.entries = append([]*MergeShadowDecision{&d}, r.entries...)
	if len(r.entries) > mergeShadowRecordLimit {
		r.entries = r.entries[:mergeShadowRecordLimit]
	}
	return true
}

// markEpisodeFalse marks the would_alert decisions of the Automaton's newest
// pending episode as found false: it walks that Automaton's decisions newest
// first while they are pending, and stops at the first that is not (the end
// of an earlier episode). The caller holds r.mu.
func (r *mergeShadowRecords) markEpisodeFalse(automatonID string) {
	for _, e := range r.entries {
		if e.AutomatonID != automatonID {
			continue
		}
		if e.State != MergeStatePending {
			return
		}
		if e.WouldAlert && !e.FoundFalse {
			e.FoundFalse = true
			r.falseCount++
			r.dirty = true
		}
	}
}

// counts returns how many shadow decisions were made and how many of them
// were later found false, over the whole history (not only the kept list).
func (r *mergeShadowRecords) counts() (total, found int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total, r.falseCount
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
	file := mergeShadowFile{
		DecisionsTotal: r.total,
		DecisionsFalse: r.falseCount,
		Decisions:      make([]MergeShadowDecision, 0, len(r.entries)),
	}
	for _, e := range r.entries {
		file.Decisions = append(file.Decisions, *e)
	}
	data, err := json.Marshal(file)
	if err == nil {
		err = atomicWriteFile(r.path, data)
	}
	if err != nil {
		log.Printf("[autonomous] Automaton merge shadow records: write %s: %v", r.path, err)
		return
	}
	r.dirty = false
}
