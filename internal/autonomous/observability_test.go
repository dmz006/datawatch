// v5.22.0 — observability fill-in tests. Verifies LoopStatus
// surfaces the recursion + guardrail counters added in this release.

package autonomous

import (
	"testing"
)

func TestStatus_ChildPRDsAndMaxDepth(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	root, _ := m.CreatePRD("root", "/p", "claude", EffortNormal)
	c1, _ := m.Store().CreatePRDWithParent("c1", "/p", "claude", EffortNormal, root.ID, "t1", 1)
	_, _ = m.Store().CreatePRDWithParent("gc1", "/p", "claude", EffortNormal, c1.ID, "t2", 2)
	_, _ = m.Store().CreatePRDWithParent("c2", "/p", "claude", EffortNormal, root.ID, "t3", 1)

	st := m.Status()
	if st.ChildPRDsTotal != 3 {
		t.Errorf("child_prds_total = %d, want 3", st.ChildPRDsTotal)
	}
	if st.MaxDepthSeen != 2 {
		t.Errorf("max_depth_seen = %d, want 2", st.MaxDepthSeen)
	}
}

func TestStatus_BlockedPRDsCount(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	for _, s := range []PRDStatus{PRDDraft, PRDBlocked, PRDBlocked, PRDCompleted} {
		p, _ := m.CreatePRD("p", "/p", "claude", EffortNormal)
		p.Status = s
		_ = m.Store().SavePRD(p)
	}
	st := m.Status()
	if st.BlockedPRDs != 2 {
		t.Errorf("blocked_prds = %d, want 2", st.BlockedPRDs)
	}
}

func TestStatus_VerdictCountsRollup(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", EffortNormal)
	stories := []Story{{
		ID:    "s1",
		Title: "S1",
		Verdicts: []GuardrailVerdict{
			{Guardrail: "rules", Outcome: "pass"},
			{Guardrail: "security", Outcome: "warn"},
		},
		Tasks: []Task{
			{
				ID: "t1", Title: "T1",
				Verdicts: []GuardrailVerdict{
					{Guardrail: "rules", Outcome: "pass"},
					{Guardrail: "release-readiness", Outcome: "block"},
				},
			},
			{ID: "t2", Title: "T2"}, // no verdicts
		},
	}}
	_ = m.Store().SetStories(prd.ID, stories)

	st := m.Status()
	if st.VerdictCounts == nil {
		t.Fatal("verdict_counts nil")
	}
	if st.VerdictCounts["pass"] != 2 {
		t.Errorf("pass = %d, want 2", st.VerdictCounts["pass"])
	}
	if st.VerdictCounts["warn"] != 1 {
		t.Errorf("warn = %d, want 1", st.VerdictCounts["warn"])
	}
	if st.VerdictCounts["block"] != 1 {
		t.Errorf("block = %d, want 1", st.VerdictCounts["block"])
	}
}

func TestStatus_NoVerdictsLeavesMapNil(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	_, _ = m.CreatePRD("p", "/p", "claude", EffortNormal)

	st := m.Status()
	if st.VerdictCounts != nil {
		t.Errorf("verdict_counts should be nil when nothing has rolled up: %v", st.VerdictCounts)
	}
}
