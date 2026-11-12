package autonomous

import (
	"fmt"
	"strings"
)

// This file derives the lane counts from the LLM concurrency caps, so the
// caps are the one source of truth: a backend that admits one session at a
// time gets one lane however many lanes are configured. The derived counts
// are built once per poll and the whole poll plans with them.

// Lane count sources reported in the status.
const (
	// LaneCountSourceCaps: the count is the sum of the LLM entry caps.
	LaneCountSourceCaps = "llm_caps"
	// LaneCountSourceConfig: the count is the configured gpu_lanes or
	// claude_lanes value.
	LaneCountSourceConfig = "config"
)

// LaneCapEntry is one LLM entry as the lane counts see it.
type LaneCapEntry struct {
	// Backend is the backend name the entry serves (the entry name).
	Backend string
	// Enabled is false for a disabled entry, which is ignored.
	Enabled bool
	// Cap is the entry's concurrency cap; 0 means unlimited.
	Cap int
}

// LaneCapSource reads the LLM entries with their effective caps. The real one
// (cmd/datawatch/lane_fill_wiring.go) applies inference.EffectiveMaxInflight,
// the function the capacity ledger limits come from, so no number is derived
// twice.
type LaneCapSource interface {
	LaneCapEntries() []LaneCapEntry
}

// LaneFillLaneCount is the lane count of one lane type: what was configured,
// what the poll used, and where the used number came from.
type LaneFillLaneCount struct {
	Configured int    `json:"configured"`
	Effective  int    `json:"effective"`
	Source     string `json:"source"`
	// Reason says why the configured count was used, when it was.
	Reason string `json:"reason,omitempty"`
}

// LaneFillLaneCounts is the lane counts of the last poll.
type LaneFillLaneCounts struct {
	GPU    LaneFillLaneCount `json:"gpu"`
	Claude LaneFillLaneCount `json:"claude"`
}

// SetLaneCapSource sets where the LLM caps come from. Without one the
// configured lane counts are used.
func (c *Coordinator) SetLaneCapSource(src LaneCapSource) {
	if c == nil {
		return
	}
	c.lf.mu.Lock()
	c.lf.caps = src
	c.lf.mu.Unlock()
}

// laneFillCapSource returns the cap source set, or nil.
func (c *Coordinator) laneFillCapSource() LaneCapSource {
	c.lf.mu.Lock()
	defer c.lf.mu.Unlock()
	return c.lf.caps
}

// deriveLaneCount works out one lane type's count. configured is the count
// from the config; backend is the backend the lane type runs.
func deriveLaneCount(fromCaps bool, src LaneCapSource, kind, backend string, configured int) LaneFillLaneCount {
	out := LaneFillLaneCount{Configured: configured, Effective: configured, Source: LaneCountSourceConfig}
	switch {
	case !fromCaps:
		out.Reason = "lane_counts_from_caps is off"
		return out
	case configured == 0:
		out.Reason = fmt.Sprintf("%s lanes are configured for none", kind)
		return out
	case src == nil:
		out.Reason = "no LLM cap source"
		return out
	}
	sum, found := 0, false
	for _, e := range src.LaneCapEntries() {
		if !e.Enabled || e.Backend != backend {
			continue
		}
		found = true
		if e.Cap <= 0 {
			out.Reason = fmt.Sprintf("a %s entry is unlimited", kind)
			return out
		}
		sum += e.Cap
	}
	if !found {
		out.Reason = fmt.Sprintf("no enabled %s entry for backend %s", kind, strings.TrimSpace(backend))
		return out
	}
	out.Effective, out.Source = sum, LaneCountSourceCaps
	return out
}

// laneFillDeriveCounts returns cfg with the lane counts the caps give, and
// the counts for the status. Every reader of one poll uses the returned
// configuration, so the poll sees one set of counts.
func laneFillDeriveCounts(cfg LaneFillConfig, src LaneCapSource) (LaneFillConfig, LaneFillLaneCounts) {
	fromCaps := cfg.EffectiveLaneCountsFromCaps()
	counts := LaneFillLaneCounts{
		GPU: deriveLaneCount(fromCaps, src, "gpu", cfg.EffectiveGPUBackend(), cfg.EffectiveGPULanes()),
		Claude: deriveLaneCount(fromCaps, src, "claude", cfg.EffectiveClaudeBackend(),
			cfg.EffectiveClaudeLanes()),
	}
	derived := cfg
	derived.GPULanes = counts.GPU.Effective
	derived.ClaudeLanes = counts.Claude.Effective
	return derived, counts
}
