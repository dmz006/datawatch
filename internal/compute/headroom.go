// Package compute — headroom gate: measurement-based cap coverage checks.
//
// This file defines the primitives an admission path needs to decide whether
// a node can safely raise a parallelism cap above 1:
//
//   - HeadroomMeasurement: measured free memory on a node with its resident
//     model loaded, plus the memory one extra parallel request needs.
//   - HeadroomStore: an in-memory, per-node store of measurements. It is
//     memory-only by design: a daemon restart clears it, so the cap falls
//     back to 1 until the next measurement arrives.
//   - CapCoverage / CapRefusal: decide whether a requested cap is supported
//     and explain a refusal.
//   - SmallestAllowedModel: pick the smallest model from a list.
//
// The gate is a pure function of its inputs. The caller passes the clock
// (now), the staleness budget (maxAge) and the free-memory floor (minFreeMB,
// see EffectiveVerifierMinFreeMB), so there is no second default here.
package compute

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// bytesToMB converts a byte count to whole megabytes (MiB), rounding down.
func bytesToMB(v uint64) uint64 {
	return v / (1 << 20)
}

// HeadroomMeasurement is one node's memory headroom snapshot. FreeMB is free
// memory measured with the resident model loaded; PerRequestMB is the memory
// one extra parallel request needs on top of that.
type HeadroomMeasurement struct {
	Node          string
	FreeMB        int
	PerRequestMB  int
	ResidentModel string
	MeasuredAt    time.Time
}

// CapRefusal reports that a requested cap was refused and the node must stay
// at cap 1. FreeMB is -1 when there was no usable measurement.
type CapRefusal struct {
	Node        string
	FreeMB      int
	ShortfallMB int
	Reason      string
}

// Error names the node, and for a measured refusal the free memory and the
// shortfall. A missing or stale measurement names the node and the reason.
func (r *CapRefusal) Error() string {
	if r.FreeMB >= 0 {
		return fmt.Sprintf("cap on node %s refused: measured free %d MB, shortfall %d MB (%s)",
			r.Node, r.FreeMB, r.ShortfallMB, r.Reason)
	}
	return fmt.Sprintf("cap on node %s refused: %s", r.Node, r.Reason)
}

// CapCoverage decides whether the requested cap is supported by the node's
// memory headroom. A cap of 1 or less is always supported with no
// measurement. For a higher cap a missing measurement, or one older than
// maxAge, is refused; otherwise (FreeMB - minFreeMB) must cover
// (cap-1)*PerRequestMB. Every refusal returns supported 1.
func CapCoverage(m *HeadroomMeasurement, cap int, minFreeMB int, now time.Time, maxAge time.Duration) (int, *CapRefusal) {
	if cap <= 1 {
		return 1, nil
	}
	if m == nil || m.MeasuredAt.IsZero() {
		node := "unknown"
		if m != nil && m.Node != "" {
			node = m.Node
		}
		return 1, &CapRefusal{Node: node, FreeMB: -1, Reason: "no fresh measurement"}
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

// HeadroomStore is an in-memory, mutex-guarded, per-node measurement store.
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
	if s.m == nil {
		s.m = make(map[string]HeadroomMeasurement)
	}
	s.m[x.Node] = x
}

// Get returns the latest measurement for node.
func (s *HeadroomStore) Get(node string) (HeadroomMeasurement, bool) {
	if s == nil {
		return HeadroomMeasurement{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.m[node]
	return x, ok
}

// ModelInfo is one model a node lists: its name (with tag) and its size in
// bytes when the node reports it (0 means unknown).
type ModelInfo struct {
	Name      string
	SizeBytes uint64
}

var tagSizeRe = regexp.MustCompile(`(?i)(?:^|[^0-9a-z.])(\d+(?:\.\d+)?)b(?:$|[^a-z0-9])`)

// tagSizeBillions parses a parameter count in billions from a model name's
// tag, such as 4b in "qwen3:4b". ok is false when there is none.
func tagSizeBillions(name string) (float64, bool) {
	tag := name
	if i := strings.LastIndex(name, ":"); i >= 0 {
		tag = name[i+1:]
	}
	mt := tagSizeRe.FindStringSubmatch(tag)
	if mt == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(mt[1], 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// modelSizeKey is the comparable size of a model: bytes when the node list
// gives them, else the tag-parsed billions scaled to bytes. ok is false when
// the size is unknown.
func modelSizeKey(m ModelInfo) (float64, bool) {
	if m.SizeBytes > 0 {
		return float64(m.SizeBytes), true
	}
	if b, ok := tagSizeBillions(m.Name); ok {
		return b * 1e9, true
	}
	return 0, false
}

// SmallestAllowedModel returns the smallest model by size (node-listed size
// when known, else the size in the tag), with name as the tiebreak. Models
// of unknown size sort after known ones and fall back to name order. ok is
// false for an empty list.
func SmallestAllowedModel(models []ModelInfo) (ModelInfo, bool) {
	if len(models) == 0 {
		return ModelInfo{}, false
	}
	best := models[0]
	for _, m := range models[1:] {
		if modelLess(m, best) {
			best = m
		}
	}
	return best, true
}

// SortSmallestFirst returns the model names ordered smallest first by the
// same rule as SmallestAllowedModel (size, then name). It is the order
// Measure expects its allow-list in: the first name is the probe model. The
// input slice is not modified.
func SortSmallestFirst(names []string) []string {
	models := make([]ModelInfo, 0, len(names))
	for _, n := range names {
		models = append(models, ModelInfo{Name: n})
	}
	sort.SliceStable(models, func(i, j int) bool { return modelLess(models[i], models[j]) })
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.Name)
	}
	return out
}

func modelLess(a, b ModelInfo) bool {
	sa, oka := modelSizeKey(a)
	sb, okb := modelSizeKey(b)
	switch {
	case oka && okb && sa != sb:
		return sa < sb
	case oka != okb:
		return oka
	}
	return a.Name < b.Name
}
