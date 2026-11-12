package autonomous

import (
	"strings"
	"sync"
	"testing"
)

// laneFillCapsFake is a cap source whose entries a test can change between polls.
type laneFillCapsFake struct {
	mu      sync.Mutex
	entries []LaneCapEntry
}

func (f *laneFillCapsFake) LaneCapEntries() []LaneCapEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]LaneCapEntry(nil), f.entries...)
}

func (f *laneFillCapsFake) set(e ...LaneCapEntry) {
	f.mu.Lock()
	f.entries = e
	f.mu.Unlock()
}

func capsConfig() LaneFillConfig {
	return LaneFillConfig{
		Mode: "shadow", GPULanes: 2, ClaudeLanes: 3,
		GPUBackend: "pi-prompt", ClaudeBackend: "claude-code",
	}
}

func TestLaneFillCapsLiveCapBelowConfiguredCount(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 1})
	derived, counts := laneFillDeriveCounts(capsConfig(), src)
	if got := derived.EffectiveClaudeLanes(); got != 1 {
		t.Fatalf("claude lanes = %d, want 1", got)
	}
	c := counts.Claude
	if c.Configured != 3 || c.Effective != 1 || c.Source != LaneCountSourceCaps || c.Reason != "" {
		t.Fatalf("claude count = %+v", c)
	}
}

func TestLaneFillCapsEntriesOnOneBackendAreSummed(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(
		LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 1},
		LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 2},
		LaneCapEntry{Backend: "other", Enabled: true, Cap: 9},
	)
	_, counts := laneFillDeriveCounts(capsConfig(), src)
	if counts.Claude.Effective != 3 || counts.Claude.Source != LaneCountSourceCaps {
		t.Fatalf("claude count = %+v, want 3 from llm_caps", counts.Claude)
	}
}

func TestLaneFillCapsDisabledEntryIsIgnored(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(
		LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 1},
		LaneCapEntry{Backend: "claude-code", Enabled: false, Cap: 5},
		LaneCapEntry{Backend: "claude-code", Enabled: false, Cap: 0}, // a disabled unlimited entry does not force the fallback
	)
	_, counts := laneFillDeriveCounts(capsConfig(), src)
	if counts.Claude.Effective != 1 || counts.Claude.Source != LaneCountSourceCaps {
		t.Fatalf("claude count = %+v, want 1 from llm_caps", counts.Claude)
	}
}

func TestLaneFillCapsUnlimitedGPUEntryFallsBackToConfigured(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(
		LaneCapEntry{Backend: "pi-prompt", Enabled: true, Cap: 0},
		LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 2},
	)
	derived, counts := laneFillDeriveCounts(capsConfig(), src)
	g := counts.GPU
	if g.Effective != 2 || g.Configured != 2 || g.Source != LaneCountSourceConfig {
		t.Fatalf("gpu count = %+v, want the configured 2", g)
	}
	if !strings.Contains(g.Reason, "a gpu entry is unlimited") {
		t.Fatalf("reason = %q", g.Reason)
	}
	if derived.EffectiveGPULanes() != 2 || derived.EffectiveClaudeLanes() != 2 {
		t.Fatalf("derived lanes gpu %d claude %d", derived.EffectiveGPULanes(), derived.EffectiveClaudeLanes())
	}
}

func TestLaneFillCapsNoMatchingEntryFallsBack(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(LaneCapEntry{Backend: "something-else", Enabled: true, Cap: 1})
	_, counts := laneFillDeriveCounts(capsConfig(), src)
	if counts.Claude.Effective != 3 || counts.Claude.Source != LaneCountSourceConfig || counts.Claude.Reason == "" {
		t.Fatalf("claude count = %+v", counts.Claude)
	}
	if counts.GPU.Effective != 2 || counts.GPU.Source != LaneCountSourceConfig || counts.GPU.Reason == "" {
		t.Fatalf("gpu count = %+v", counts.GPU)
	}
	// A missing source falls back the same way.
	_, none := laneFillDeriveCounts(capsConfig(), nil)
	if none.Claude.Effective != 3 || none.Claude.Source != LaneCountSourceConfig || none.Claude.Reason == "" {
		t.Fatalf("no source: claude count = %+v", none.Claude)
	}
}

func TestLaneFillCapsKeyFalseKeepsConfiguredCounts(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 1})
	cfg := capsConfig()
	off := false
	cfg.LaneCountsFromCaps = &off
	derived, counts := laneFillDeriveCounts(cfg, src)
	if derived.EffectiveClaudeLanes() != 3 || counts.Claude.Source != LaneCountSourceConfig || counts.Claude.Effective != 3 {
		t.Fatalf("claude count = %+v", counts.Claude)
	}
}

func TestLaneFillCapsZeroConfiguredStaysNone(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 2})
	cfg := capsConfig()
	cfg.ClaudeLanes = 0
	derived, counts := laneFillDeriveCounts(cfg, src)
	if derived.EffectiveClaudeLanes() != 0 || counts.Claude.Source != LaneCountSourceConfig {
		t.Fatalf("a claude_lanes of 0 must stay none: %+v", counts.Claude)
	}
}

func TestLaneFillCapsLoweredCapChangesNextPollLanes(t *testing.T) {
	cfg := laneFillCoordConfig("shadow")
	cfg.LaneFill.ClaudeLanes = 3
	h := newLaneFillHarnessIn(t, t.TempDir(), cfg)
	h.c.SetReady()
	src := &laneFillCapsFake{}
	src.set(LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 3})
	h.c.SetLaneCapSource(src)

	claudeLanes := func() int {
		n := 0
		for _, l := range h.status().Lanes {
			if l.Type == LaneTypeClaude {
				n++
			}
		}
		return n
	}
	h.poll()
	if got := claudeLanes(); got != 3 {
		t.Fatalf("first poll: %d claude lanes, want 3", got)
	}
	st := h.status()
	if st.LaneCounts.Claude.Effective != 3 || st.LaneCounts.Claude.Source != LaneCountSourceCaps {
		t.Fatalf("first poll counts = %+v", st.LaneCounts)
	}

	src.set(LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 1})
	h.poll()
	if got := claudeLanes(); got != 1 {
		t.Fatalf("second poll: %d claude lanes, want 1", got)
	}
	st = h.status()
	if st.LaneCounts.Claude.Configured != 3 || st.LaneCounts.Claude.Effective != 1 {
		t.Fatalf("second poll counts = %+v", st.LaneCounts)
	}
}

func TestLaneFillCapsStatusBeforeFirstPollShowsCounts(t *testing.T) {
	cfg := laneFillCoordConfig("shadow")
	cfg.LaneFill.ClaudeLanes = 3
	h := newLaneFillHarnessIn(t, t.TempDir(), cfg)
	src := &laneFillCapsFake{}
	src.set(LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 1})
	h.c.SetLaneCapSource(src)
	st := h.status()
	if st.LaneCounts.Claude.Effective != 1 || st.LaneCounts.Claude.Source != LaneCountSourceCaps {
		t.Fatalf("counts = %+v", st.LaneCounts)
	}
}

func TestLaneFillCapsOneClaudeLaneNeverPlansTwoStarts(t *testing.T) {
	src := &laneFillCapsFake{}
	src.set(LaneCapEntry{Backend: "claude-code", Enabled: true, Cap: 1})
	cfg, _ := laneFillDeriveCounts(capsConfig(), src)
	lanes := BuildLanes(cfg, nil, LaneEvidence{}).Lanes
	cands := []LaneFillCandidate{
		readyCand("aaaa0001", "claude", 0, 1),
		readyCand("aaaa0002", "claude", 0, 2),
		readyCand("aaaa0003", "claude", 0, 3),
	}
	starts := 0
	for _, d := range PlanLaneFill(LaneFillInput{Config: cfg, Lanes: lanes, Candidates: cands, Now: laneFillTestNow}) {
		if d.Action == LaneFillActionStartPRD && d.LaneType == LaneTypeClaude {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("planned %d claude starts with 1 claude lane, want 1", starts)
	}
}
