// Package compute — headroom gate: measurement-based cap coverage checks.
//
// This file adds nothing to the node registry itself; it defines the
// primitives an admission path (verifier, spawn, or any consumer of a
// Node's parallelism cap) needs to decide whether a node can safely
// raise a cap above 1:
//
//   - HeadroomMeasurement: measured free memory on a node with its
//     resident model loaded, plus the memory one extra parallel request
//     needs.
//   - CapCoverage: given a fresh-enough measurement, decide whether the
//     requested cap is supported or must fall back to 1.
//   - CapRefusal: the refusal a consumer reports when a cap is refused.
//   - HeadroomStore: an in-memory, per-node measurement store. It is
//     memory-only by design: a daemon restart clears it, so the cap
//     falls back to 1 until the next measurement arrives.
//
// The gate is purely a function of its inputs — callers pass the clock
// forward (now) and the staleness budget (maxAge), so tests and
// production share one code path and tests never depend on wall time.

package compute

import (
	"fmt"
	"sync"
	"time"
)

// HeadroomMeasurement is one node's memory headroom snapshot.
//
// FreeMB is free video memory (or free system memory when the node has
// no GPU data) measured with the resident model loaded. PerRequestMB is
// the memory one extra parallel request needs on top of that.
type HeadroomMeasurement struct {
	Node          string
	FreeMB        int
	PerRequestMB  int
	ResidentModel string
	MeasuredAt    time.Time
}

// CapRefusal reports that a requested cap was refused and the node must
// stay at cap 1.
type CapRefusal struct {
	Node        string
	FreeMB      int
	ShortfallMB int
	Reason      string
}

// Error names the node, the measured free memory and the shortfall,
// e.g. "verifier cap 2 on node gpu-a refused: measured free 3000 MB,
// need 4500 MB, shortfall 1500 MB". For a missing or stale measurement
// it still names the node and gives the reason.
func (r *CapRefusal) Error() string {
	if r.FreeMB >= 0 {
		return fmt.Sprintf("verifier cap on node %s refused: measured free %d MB, shortfall %d MB (%s)",
			r.Node, r.FreeMB, r.ShortfallMB, r.Reason)
	}
	return fmt.Sprintf("verifier cap on node %s refused: %s", r.Node, r.Reason)
}

// CapCoverage decides whether node's requested cap is supported by its
// memory headroom.
//
// A cap of 1 or less is always supported and returns (1, nil) with no
// measurement required. For a cap above 1:
//
//   - no measurement at all is refused with reason "no fresh measurement";
//   - a measurement older than maxAge (now - MeasuredAt > maxAge) is
//     refused with reason "measurement stale";
//   - otherwise the gate needs (cap-1)*PerRequestMB bytes beyond the
//     minFreeMB floor: if FreeMB - minFreeMB is less than that, it is
//     refused with the shortfall; if it covers, the cap is supported.
//
// Every refusal returns supported 1, the fallback the node must run at.
// The clock is passed in, never read: tests drive it with fixed times.
func CapCoverage(m *HeadroomMeasurement, cap int, minFreeMB int, now time.Time, maxAge time.Duration) (int, *CapRefusal) {
	if cap <= 1 {
		return 1, nil
	}
	if m == nil || m.MeasuredAt.IsZero() {
		return 1, &CapRefusal{Node: nodeOrUnknown(m), Reason: "no fresh measurement", FreeMB: -1}
	}
	if now.Sub(m.MeasuredAt) > maxAge {
		return 1, &CapRefusal{Node: m.Node, FreeMB: -1, Reason: "measurement stale"}
	}
	needed := (cap - 1) * m.PerRequestMB
	available := m.FreeMB - minFreeMB
	if available < needed {
		return 1, &CapRefusal{Node: m.Node, FreeMB: m.FreeMB, ShortfallMB: needed - available, Reason: "insufficient free memory"}
	}
	return cap, nil
}

func nodeOrUnknown(m *HeadroomMeasurement) string {
	if m != nil && m.Node != "" {
		return m.Node
	}
	return "unknown"
}

// HeadroomStore is an in-memory, per-node store of headroom
// measurements. It is not persisted: a daemon restart clears it, so the
// cap falls back to 1 until the next measurement is Put.
type HeadroomStore struct {
	mu sync.Mutex
	m  map[string]HeadroomMeasurement
}

// NewHeadroomStore returns a ready-to-use store.
func NewHeadroomStore() *HeadroomStore {
	return &HeadroomStore{m: make(map[string]HeadroomMeasurement)}
}

// Put records the measurement for its node, replacing any earlier one.
func (s *HeadroomStore) Put(x HeadroomMeasurement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[x.Node] = x
}

// Get returns the latest measurement for node.
func (s *HeadroomStore) Get(node string) (HeadroomMeasurement, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.m[node]
	return x, ok
}
