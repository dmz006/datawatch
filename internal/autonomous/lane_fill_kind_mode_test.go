package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// kindModeHarness builds a coordinator with one GPU lane and one Claude lane
// and a Ready state, and records every audit action it writes.
func kindModeHarness(t *testing.T, base, gpuMode, claudeMode string) *laneFillHarness {
	t.Helper()
	cfg := laneFillCoordConfig(base)
	cfg.LaneFill.GPUMode, cfg.LaneFill.ClaudeMode = gpuMode, claudeMode
	h := newLaneFillHarness(t, base)
	h.m.SetConfig(cfg)
	h.c.SetReady()
	return h
}

func (h *laneFillHarness) setKindModes(base, gpuMode, claudeMode string) {
	cfg := h.m.Config()
	cfg.LaneFill.Mode, cfg.LaneFill.GPUMode, cfg.LaneFill.ClaudeMode = base, gpuMode, claudeMode
	h.m.SetConfig(cfg)
}

// captureAll replaces the harness audit hook with one that keeps every action.
func (h *laneFillHarness) captureAll() *[]auditEntry {
	var got []auditEntry
	h.c.auditFn = func(action string, d map[string]any) {
		h.mu.Lock()
		defer h.mu.Unlock()
		got = append(got, auditEntry{action: action, details: d})
	}
	return &got
}

type auditEntry struct {
	action  string
	details map[string]any
}

func countAudit(h *laneFillHarness, es *[]auditEntry, action string) []auditEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []auditEntry
	for _, e := range *es {
		if e.action == action {
			out = append(out, e)
		}
	}
	return out
}

func callsWithPrefix(h *laneFillHarness, prefix string) []string {
	var out []string
	for _, c := range h.fake.all() {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func TestLaneFillKindModeDefaultConfigExecutesNothing(t *testing.T) {
	cfg := DefaultConfig()
	h := newLaneFillHarnessIn(t, t.TempDir(), cfg)
	h.c.SetReady()
	h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h.addPRD("draft work", PRDDraft, "", "")

	h.poll()

	st := h.status()
	if st.Mode != "shadow" || st.Modes.GPU != "shadow" || st.Modes.Claude != "shadow" {
		t.Fatalf("default modes = %q / %+v, want shadow for both kinds", st.Mode, st.Modes)
	}
	if len(st.Decisions) == 0 {
		t.Fatal("a shadow poll recorded no decisions")
	}
	for _, d := range st.Decisions {
		if d.Executed || d.Error != "" || d.Mode != LaneFillModeShadow {
			t.Errorf("default-config decision executed or not shadow: %+v", d)
		}
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("default config made starter calls: %v", calls)
	}
}

func TestLaneFillKindModeGPUOnClaudeInheritsShadow(t *testing.T) {
	h := kindModeHarness(t, "shadow", "on", "")
	gpu := h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	cl := h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	h.poll()

	if got := h.fake.all(); len(got) != 1 || got[0] != "run:"+gpu.ID {
		t.Fatalf("calls = %v, want only the GPU start", got)
	}
	st := h.status()
	g := findDecision(t, st, "gpu-1", LaneFillActionStartPRD)
	c := findDecision(t, st, "claude-1", LaneFillActionStartPRD)
	if !g.Executed || g.Mode != LaneFillModeOn || g.PRDID != gpu.ID {
		t.Errorf("gpu decision = %+v", g)
	}
	if c.Executed || c.Mode != LaneFillModeShadow || c.PRDID != cl.ID {
		t.Errorf("claude decision should be recorded unexecuted in shadow: %+v", c)
	}
}

func TestLaneFillKindModeClaudeOnGPUInheritsShadow(t *testing.T) {
	h := kindModeHarness(t, "shadow", "", "on")
	h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	cl := h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	h.poll()

	if got := h.fake.all(); len(got) != 1 || got[0] != "run:"+cl.ID {
		t.Fatalf("calls = %v, want only the Claude start", got)
	}
	st := h.status()
	g := findDecision(t, st, "gpu-1", LaneFillActionStartPRD)
	c := findDecision(t, st, "claude-1", LaneFillActionStartPRD)
	if g.Executed || g.Mode != LaneFillModeShadow {
		t.Errorf("gpu decision should be recorded unexecuted in shadow: %+v", g)
	}
	if !c.Executed || c.Mode != LaneFillModeOn {
		t.Errorf("claude decision = %+v", c)
	}
}

func TestLaneFillKindModeClaudeOffDropsClaudeDecisions(t *testing.T) {
	h := kindModeHarness(t, "shadow", "", "off")
	h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	h.poll()

	st := h.status()
	for _, d := range st.Decisions {
		if d.LaneType == LaneTypeClaude || strings.HasPrefix(d.LaneID, "claude") {
			t.Errorf("a Claude decision was recorded with the Claude kind off: %+v", d)
		}
	}
	findDecision(t, st, "gpu-1", LaneFillActionStartPRD)
	for _, l := range st.Lanes {
		if l.Type == LaneTypeClaude {
			t.Errorf("Claude lane shown with the kind off: %+v", l)
		}
	}
	if st.QueuedEligible.Claude != 0 || st.Modes.Claude != "off" || st.Modes.GPU != "shadow" {
		t.Errorf("queued %+v modes %+v", st.QueuedEligible, st.Modes)
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestLaneFillKindModeOffKindWithOtherKindOn(t *testing.T) {
	h := kindModeHarness(t, "shadow", "on", "off")
	gpu := h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	h.poll()

	if got := h.fake.all(); len(got) != 1 || got[0] != "run:"+gpu.ID {
		t.Fatalf("calls = %v, want only the GPU start", got)
	}
	for _, d := range h.status().Decisions {
		if d.LaneType == LaneTypeClaude {
			t.Errorf("Claude decision recorded: %+v", d)
		}
	}
}

func TestLaneFillKindModeOverridesBaseInBothDirections(t *testing.T) {
	// Base on, GPU kind shadow: the GPU lane records but does not run.
	h := kindModeHarness(t, "on", "shadow", "")
	h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	cl := h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h.poll()
	if got := h.fake.all(); len(got) != 1 || got[0] != "run:"+cl.ID {
		t.Fatalf("base on, gpu_mode shadow: calls = %v, want only the Claude start", got)
	}
	st := h.status()
	if g := findDecision(t, st, "gpu-1", LaneFillActionStartPRD); g.Executed || g.Mode != LaneFillModeShadow {
		t.Errorf("gpu decision = %+v", g)
	}
	if st.Mode != "on" || st.Modes.GPU != "shadow" || st.Modes.Claude != "on" {
		t.Errorf("status modes = %q %+v", st.Mode, st.Modes)
	}

	// Base shadow, GPU kind on: covered above; base off, GPU kind on runs GPU only.
	h2 := kindModeHarness(t, "off", "on", "")
	gpu := h2.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	h2.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h2.poll()
	if got := h2.fake.all(); len(got) != 1 || got[0] != "run:"+gpu.ID {
		t.Fatalf("base off, gpu_mode on: calls = %v, want only the GPU start", got)
	}
	st2 := h2.status()
	if st2.Mode != "off" || st2.Modes.GPU != "on" || st2.Modes.Claude != "off" {
		t.Errorf("status modes = %q %+v", st2.Mode, st2.Modes)
	}
	for _, d := range st2.Decisions {
		if d.LaneType == LaneTypeClaude {
			t.Errorf("Claude decision recorded with the kind off: %+v", d)
		}
	}
}

func TestLaneFillSwitchBackToShadowStopsExecution(t *testing.T) {
	h := kindModeHarness(t, "shadow", "on", "")
	first := h.addPRD("gpu work one", PRDApproved, "gpu", "pi-prompt")
	h.poll()
	if got := h.fake.all(); len(got) != 1 || got[0] != "run:"+first.ID {
		t.Fatalf("calls = %v", got)
	}

	// Flip the GPU kind back to shadow while a second Automaton waits and
	// the first is still running: nothing more runs, nothing is touched.
	h.setKindModes("shadow", "shadow", "")
	second := h.addPRD("gpu work two", PRDApproved, "gpu", "pi-prompt")
	h.m.store.MutatePRD(first.ID, func(p *PRD) bool { p.Status = PRDCompleted; return true })
	h.now = h.now.Add(time.Minute)
	h.poll()

	if got := h.fake.all(); len(got) != 1 {
		t.Fatalf("calls after the switch back to shadow = %v", got)
	}
	d := findDecision(t, h.status(), "gpu-1", LaneFillActionStartPRD)
	if d.PRDID != second.ID || d.Executed || d.Mode != LaneFillModeShadow {
		t.Errorf("decision after the switch = %+v", d)
	}
	if p, _ := h.m.store.GetPRD(first.ID); p.Status != PRDCompleted {
		t.Errorf("an already started Automaton was touched: %v", p.Status)
	}
}

func TestLaneFillSwitchRefusedAutomatonNeverExecutesWithGPUOn(t *testing.T) {
	h := kindModeHarness(t, "shadow", "on", "")
	held := h.addPRD("held gpu work", PRDApproved, "gpu", "pi-prompt")
	held.Spec = "HOLD: the pool driver is not ready yet.\nThe rest is unchanged."
	if err := h.m.store.SavePRD(held); err != nil {
		t.Fatal(err)
	}

	h.poll()
	h.now = h.now.Add(laneFillRetryAfter + time.Minute)
	h.poll()

	if got := h.fake.all(); len(got) != 0 {
		t.Fatalf("a refused Automaton reached the starter: %v", got)
	}
	st := h.status()
	var blocked bool
	for _, d := range st.Decisions {
		if d.Action == LaneFillActionStartPRD || d.Action == LaneFillActionDecompose {
			if d.PRDID == held.ID {
				t.Errorf("a refused Automaton was planned: %+v", d)
			}
		}
		if d.PRDID == held.ID && d.Action == LaneFillActionBlocked {
			blocked = true
			if d.Executed || d.EligibilityReason != LaneFillReasonHeld {
				t.Errorf("refusal record = %+v", d)
			}
		}
	}
	if !blocked {
		t.Errorf("no blocked decision for the refused Automaton: %+v", st.Decisions)
	}
	if p, _ := h.m.store.GetPRD(held.ID); p.Status != PRDApproved {
		t.Errorf("refused Automaton changed status: %v", p.Status)
	}
}

func TestLaneFillSwitchAuditWrittenOncePerChange(t *testing.T) {
	h := kindModeHarness(t, "shadow", "", "")
	got := h.captureAll()

	h.poll() // first poll: no entry
	if n := len(countAudit(h, got, "lane_fill_mode")); n != 0 {
		t.Fatalf("%d lane_fill_mode entries on the first poll", n)
	}
	h.now = h.now.Add(time.Minute)
	h.poll() // unchanged
	if n := len(countAudit(h, got, "lane_fill_mode")); n != 0 {
		t.Fatalf("%d lane_fill_mode entries with no change", n)
	}

	h.setKindModes("shadow", "on", "")
	h.now = h.now.Add(time.Minute)
	h.poll()
	h.now = h.now.Add(time.Minute)
	h.poll() // same mode again: still one entry
	es := countAudit(h, got, "lane_fill_mode")
	if len(es) != 1 {
		t.Fatalf("entries after gpu shadow->on = %d, want 1: %+v", len(es), es)
	}
	d := es[0].details
	if d["kind"] != "gpu" || d["old_mode"] != "shadow" || d["new_mode"] != "on" {
		t.Errorf("entry = %+v", d)
	}

	// Back to shadow, and the Claude kind off: one entry each.
	h.setKindModes("shadow", "shadow", "off")
	h.now = h.now.Add(time.Minute)
	h.poll()
	es = countAudit(h, got, "lane_fill_mode")
	if len(es) != 3 {
		t.Fatalf("entries = %d, want 3: %+v", len(es), es)
	}
	seen := map[string]string{}
	for _, e := range es[1:] {
		seen[e.details["kind"].(string)] = e.details["old_mode"].(string) + ">" + e.details["new_mode"].(string)
	}
	if seen["gpu"] != "on>shadow" || seen["claude"] != "shadow>off" {
		t.Errorf("entries = %+v", seen)
	}

	// A change of the base mode that moves an inheriting kind is audited too.
	h.setKindModes("on", "shadow", "off")
	h.now = h.now.Add(time.Minute)
	h.poll()
	if n := len(countAudit(h, got, "lane_fill_mode")); n != 3 {
		t.Fatalf("a base change that moved no kind wrote an entry: %d", n)
	}
	h.setKindModes("on", "", "")
	h.now = h.now.Add(time.Minute)
	h.poll()
	if n := len(countAudit(h, got, "lane_fill_mode")); n != 5 {
		t.Fatalf("entries after inheriting the base = %d, want 5", n)
	}
}

func TestLaneFillSwitchAuditNotWrittenOnFirstPollEvenWhenOn(t *testing.T) {
	h := kindModeHarness(t, "shadow", "on", "on")
	got := h.captureAll()
	h.poll()
	if n := len(countAudit(h, got, "lane_fill_mode")); n != 0 {
		t.Fatalf("%d lane_fill_mode entries on the first poll", n)
	}
}

func TestLaneFillKindModeStatusModesObject(t *testing.T) {
	cases := []struct {
		base, gpu, claude string
		wantBase          string
		wantGPU, wantCl   string
	}{
		{"", "", "", "shadow", "shadow", "shadow"},
		{"shadow", "on", "", "shadow", "on", "shadow"},
		{"on", "shadow", "off", "on", "shadow", "off"},
		{"off", "on", "on", "off", "on", "on"},
	}
	for _, c := range cases {
		h := kindModeHarness(t, c.base, c.gpu, c.claude)
		// Before any poll, and after one, the object reads the same.
		for _, polled := range []bool{false, true} {
			if polled {
				h.poll()
			}
			st := h.status()
			if st.Mode != c.wantBase || st.Modes.GPU != c.wantGPU || st.Modes.Claude != c.wantCl {
				t.Errorf("%+v polled=%v: mode %q modes %+v", c, polled, st.Mode, st.Modes)
			}
		}
	}
	// Without a coordinator the default reads shadow for both kinds.
	var m *Manager
	st := m.LaneFillStatus(0)
	if st.Modes.GPU != "shadow" || st.Modes.Claude != "shadow" {
		t.Errorf("nil manager modes = %+v", st.Modes)
	}
}

func TestLaneFillKindModeFailedStartKeepsRetryBackoff(t *testing.T) {
	h := kindModeHarness(t, "shadow", "on", "")
	h.fake.runErr = errors.New("capacity held")
	gpu := h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")

	h.poll()
	d := findDecision(t, h.status(), "gpu-1", LaneFillActionStartPRD)
	if d.Executed || !strings.Contains(d.Error, "capacity held") || d.Mode != LaneFillModeOn {
		t.Fatalf("failed start not recorded: %+v", d)
	}
	h.now = h.now.Add(time.Minute)
	h.poll()
	if n := len(callsWithPrefix(h, "run:")); n != 1 {
		t.Fatalf("retried inside the back-off window: %v", h.fake.all())
	}
	h.now = h.now.Add(laneFillRetryAfter)
	h.fake.runErr = nil
	h.poll()
	if got := callsWithPrefix(h, "run:"); len(got) != 2 || got[1] != "run:"+gpu.ID {
		t.Fatalf("calls after the window = %v", h.fake.all())
	}
	d = findDecision(t, h.status(), "gpu-1", LaneFillActionStartPRD)
	if !d.Executed || d.Error != "" {
		t.Errorf("retry not recorded as executed: %+v", d)
	}
}

func TestLaneFillKindModeDecomposeFollowsKindMode(t *testing.T) {
	h := kindModeHarness(t, "shadow", "on", "")
	d1 := h.addPRD("draft work", PRDDraft, "gpu", "")
	h.poll()
	if got := h.fake.all(); len(got) != 1 || got[0] != "decompose:"+d1.ID {
		t.Fatalf("calls = %v, want the GPU decompose", got)
	}

	h2 := kindModeHarness(t, "shadow", "", "on")
	h2.addPRD("draft work", PRDDraft, "gpu", "")
	h2.poll()
	if got := h2.fake.all(); len(got) != 0 {
		t.Fatalf("a GPU draft was decomposed with only the Claude kind on: %v", got)
	}
}
