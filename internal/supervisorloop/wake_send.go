// What a wake types, and the memory that keeps it from typing the same thing
// twice. Every line typed into a session costs a model turn, so a batch that
// tells the session nothing new is not typed.

package supervisorloop

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// WakeKindPriorityStall is the event kind whose entries form the priority
// stall list.
const WakeKindPriorityStall = string(TriggerPriorityStall)

// stallBlocker is the blocker code of a priority stall event: the first part
// of its fingerprint (the rest holds details such as the waiting time).
func (e WakeEvent) stallBlocker() string {
	b, _, _ := strings.Cut(e.Fingerprint, "|")
	return b
}

// WakeStallEntry is one entry of the priority stall list.
func WakeStallEntry(automatonID, blocker string) string { return automatonID + "=" + blocker }

// Line is the event's part of the typed wake line.
func (e WakeEvent) Line() string {
	p := e.Kind
	if e.AutomatonID != "" {
		p += " automaton=" + e.AutomatonID
	}
	if e.TaskID != "" {
		p += " task=" + e.TaskID
	}
	if e.Kind == WakeKindPriorityStall {
		if b := e.stallBlocker(); b != "" {
			p += " blocker=" + b
		}
	}
	return p
}

// wakeContent identifies what a batch says, whatever its order.
func wakeContent(events []WakeEvent) string {
	lines := make([]string, len(events))
	for i, e := range events {
		lines[i] = e.Line()
	}
	sort.Strings(lines)
	return strings.Join(lines, "; ")
}

// WakeSendMemory remembers what was typed into each session. It is held by
// the owner of the wake set, so replacing the set does not forget it. It is
// in memory only.
type WakeSendMemory struct {
	mu      sync.Mutex
	batches map[string]map[string]time.Time // session -> batch content -> typed at
	stalls  map[string]string               // session -> the stall list as last typed
}

// NewWakeSendMemory builds an empty memory.
func NewWakeSendMemory() *WakeSendMemory {
	return &WakeSendMemory{batches: map[string]map[string]time.Time{}, stalls: map[string]string{}}
}

// Select returns the events of a batch still worth typing into session at
// now, in their order, and a function that records them as typed (call it
// only after the input was sent). Two rules apply:
//
//   - the same batch content is not typed twice into the same session within
//     interval (zero or less selects the default fallback interval);
//   - priority stall entries are typed only when the priority stall list
//     differs from the one last typed into that session, however long ago.
//     The list is currentStalls (entries built by WakeStallEntry) together
//     with the batch's own entries.
//
// Lines that repeat inside one batch are typed once. With nothing left to
// type the result is empty.
func (m *WakeSendMemory) Select(session string, events []WakeEvent, currentStalls []string, interval time.Duration, now time.Time) ([]WakeEvent, func()) {
	nothing := func() {}
	if interval <= 0 {
		interval = DefaultWakeFallbackIntervalMinutes * time.Minute
	}
	seen := map[string]bool{}
	var batch []WakeEvent
	list := map[string]bool{}
	hasStall := false
	for _, e := range events {
		if e.Kind == WakeKindPriorityStall {
			hasStall = true
			list[WakeStallEntry(e.AutomatonID, e.stallBlocker())] = true
		}
		if l := e.Line(); !seen[l] {
			seen[l] = true
			batch = append(batch, e)
		}
	}
	for _, s := range currentStalls {
		list[s] = true
	}
	entries := make([]string, 0, len(list))
	for s := range list {
		entries = append(entries, s)
	}
	sort.Strings(entries)
	stallList := strings.Join(entries, "\n")

	m.mu.Lock()
	defer m.mu.Unlock()
	recent := m.batches[session]
	for content, at := range recent {
		if now.Sub(at) >= interval {
			delete(recent, content)
		}
	}
	whole := wakeContent(batch)
	if _, dup := recent[whole]; dup || len(batch) == 0 {
		return nil, nothing
	}
	keep := batch
	sendStalls := hasStall && m.stalls[session] != stallList
	if hasStall && !sendStalls {
		keep = nil
		for _, e := range batch {
			if e.Kind != WakeKindPriorityStall {
				keep = append(keep, e)
			}
		}
	}
	typed := wakeContent(keep)
	if _, dup := recent[typed]; dup || len(keep) == 0 {
		return nil, nothing
	}
	return keep, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.batches[session] == nil {
			m.batches[session] = map[string]time.Time{}
		}
		m.batches[session][whole] = now
		m.batches[session][typed] = now
		if sendStalls {
			m.stalls[session] = stallList
		}
	}
}
