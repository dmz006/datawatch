package autonomous

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// laneFillRecordLimit bounds the decision history kept in memory and on disk.
const laneFillRecordLimit = 1000

// laneFillRecordsFile sits beside the other autonomous stores.
const laneFillRecordsFile = "lane_fill_decisions.json"

// laneFillEntry is one stored decision plus the key it was deduplicated on.
// The key is built from the decision as the planner made it, so an error or
// blocker the executor writes into the record later does not make the next
// poll look like a different decision.
type laneFillEntry struct {
	d   LaneFillDecision
	key string
	// attemptAt is when execution was last tried (zero when never tried); it
	// paces the retry of a failed attempt. Not persisted.
	attemptAt time.Time
}

// laneFillRecords is the bounded, persisted decision history. A decision is
// appended only when it differs from the previous poll's decision for the same
// slot (a lane, or for a decision with no lane the action and Automaton);
// otherwise the latest record just has its last_seen_at and repeat_count
// advanced. Entries are kept newest first.
type laneFillRecords struct {
	mu      sync.Mutex
	path    string
	entries []*laneFillEntry
	// prev holds the entries seen in the previous poll, by slot; cur the ones
	// seen so far in the running poll.
	prev, cur map[string]*laneFillEntry
	dirty     bool
}

// newLaneFillRecords opens the history under dir. A missing file is an empty
// history; a corrupt one is logged and replaced with an empty history, never
// fatal.
func newLaneFillRecords(dir string) *laneFillRecords {
	r := &laneFillRecords{
		path: filepath.Join(dir, laneFillRecordsFile),
		prev: map[string]*laneFillEntry{},
		cur:  map[string]*laneFillEntry{},
	}
	r.load()
	return r
}

func (r *laneFillRecords) load() {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] lane fill records: read %s: %v", r.path, err)
		}
		return
	}
	var list []LaneFillDecision
	if err := json.Unmarshal(data, &list); err != nil {
		log.Printf("[autonomous] lane fill records: %s is corrupt, starting empty: %v", r.path, err)
		r.dirty = true
		r.save()
		return
	}
	if len(list) > laneFillRecordLimit {
		list = list[:laneFillRecordLimit]
	}
	for _, d := range list {
		r.entries = append(r.entries, &laneFillEntry{d: d, key: laneFillDecisionKey(d)})
	}
}

// laneFillSlotKey names the thing a decision is about: its lane, or for a
// decision with no lane (a merge report, a blocked Automaton, a decompose
// ahead) the action, Automaton and lane type.
func laneFillSlotKey(d LaneFillDecision) string {
	if d.LaneID != "" {
		return d.LaneID
	}
	return fmt.Sprintf("-|%s|%s|%s", d.Action, d.PRDID, d.LaneType)
}

// laneFillDecisionKey is what makes two decisions the same one: lane, action,
// Automaton, blocker, wait reason, and the mode they were made in. The wait
// reason is in the key, so a lane whose occupant goes from working to waiting
// starts a new record instead of folding the wait in over the old decision.
func laneFillDecisionKey(d LaneFillDecision) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s", d.LaneID, d.Action, d.PRDID, d.Blocker, d.WaitReason, d.Mode)
}

// observe stores one decision from the running poll. It returns the entry that
// stands for it and whether a new record was appended.
func (r *laneFillRecords) observe(d LaneFillDecision, now time.Time) (e *laneFillEntry, appended bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot, key := laneFillSlotKey(d), laneFillDecisionKey(d)
	if p := r.prev[slot]; p != nil && p.key == key {
		p.d.LastSeenAt = now
		p.d.RepeatCount++
		r.cur[slot] = p
		r.dirty = true
		return p, false
	}
	d.RepeatCount, d.LastSeenAt = 1, now
	e = &laneFillEntry{d: d, key: key}
	r.entries = append([]*laneFillEntry{e}, r.entries...)
	if len(r.entries) > laneFillRecordLimit {
		r.entries = r.entries[:laneFillRecordLimit]
	}
	r.cur[slot] = e
	r.dirty = true
	return e, true
}

// endPoll closes a poll: the slots seen in it become the previous poll's, so
// a lane that stops getting a decision starts a new record the next time it
// does, and the history is written out when it changed.
func (r *laneFillRecords) endPoll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prev, r.cur = r.cur, map[string]*laneFillEntry{}
	r.save()
}

// update changes a stored record under the lock (the executor's result).
func (r *laneFillRecords) update(e *laneFillEntry, f func(d *LaneFillDecision, attemptAt *time.Time)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(&e.d, &e.attemptAt)
	r.dirty = true
	r.save()
}

// read runs f over a stored record under the lock.
func (r *laneFillRecords) read(e *laneFillEntry, f func(d LaneFillDecision, attemptAt time.Time)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(e.d, e.attemptAt)
}

// list returns up to limit decisions, newest first (all of them when limit is
// not positive).
func (r *laneFillRecords) list(limit int) []LaneFillDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.entries)
	if limit > 0 && limit < n {
		n = limit
	}
	out := make([]LaneFillDecision, 0, n)
	for _, e := range r.entries[:n] {
		out = append(out, e.d)
	}
	return out
}

// save writes the history when it changed. The caller holds r.mu.
func (r *laneFillRecords) save() {
	if !r.dirty {
		return
	}
	list := make([]LaneFillDecision, 0, len(r.entries))
	for _, e := range r.entries {
		list = append(list, e.d)
	}
	data, err := json.Marshal(list)
	if err == nil {
		err = atomicWriteFile(r.path, data)
	}
	if err != nil {
		log.Printf("[autonomous] lane fill records: write %s: %v", r.path, err)
		return
	}
	r.dirty = false
}
