// BL191 Q4 (v5.9.0) — recursive child-PRD spawn shortcut tests.

package autonomous

import (
	"context"
	"strings"
	"testing"
)

// fakeDecompose returns a single-story / single-task decomposition for
// any spec. The child task itself is a leaf (SpawnPRD=false) so the
// recursion terminates one level deep.
func fakeDecompose(req DecomposeRequest) (string, error) {
	return `{"title":"child", "stories":[{"title":"S","tasks":[{"title":"leaf","spec":"work"}]}]}`, nil
}

// fakeSpawn marks every task complete with a synthetic session ID.
func fakeSpawn(_ context.Context, req SpawnRequest) (SpawnResult, error) {
	return SpawnResult{SessionID: "ses_" + req.TaskID}, nil
}

// fakeVerify always passes.
func fakeVerify(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
	return VerificationResult{OK: true, Summary: "fake-pass"}, nil
}

func TestRecurse_SpawnPRD_ChildCompletesParentTask(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRecursionDepth = 3
	cfg.AutoApproveChildren = true
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)

	parent, _ := m.CreatePRD("parent spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(parent.ID, []Story{{
		Title: "S",
		Tasks: []Task{{
			Title:    "spawn-child",
			Spec:     "build child feature foo",
			SpawnPRD: true,
		}},
	}})
	parent, _ = m.Store().GetPRD(parent.ID)
	parent.Status = PRDApproved
	_ = m.Store().SavePRD(parent)

	if err := m.Run(context.Background(), parent.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	parent, _ = m.Store().GetPRD(parent.ID)
	if parent.Status != PRDCompleted {
		t.Fatalf("parent status = %s, want completed", parent.Status)
	}
	// Parent task should reference a ChildPRDID and have a verification.
	pt := parent.Story[0].Tasks[0]
	if pt.ChildPRDID == "" {
		t.Fatal("parent task ChildPRDID empty — recursion didn't spawn")
	}
	if pt.Status != TaskCompleted {
		t.Fatalf("parent task status = %s, want completed", pt.Status)
	}
	child, ok := m.Store().GetPRD(pt.ChildPRDID)
	if !ok {
		t.Fatalf("child PRD %s not found", pt.ChildPRDID)
	}
	if child.ParentPRDID != parent.ID || child.ParentTaskID != pt.ID {
		t.Fatalf("child genealogy wrong: parent=%q task=%q", child.ParentPRDID, child.ParentTaskID)
	}
	if child.Depth != 1 {
		t.Fatalf("child depth = %d, want 1", child.Depth)
	}
	if child.Status != PRDCompleted {
		t.Fatalf("child status = %s, want completed", child.Status)
	}
}

func TestRecurse_DepthLimit_RefusesAtMax(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRecursionDepth = 2
	cfg.AutoApproveChildren = true
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)

	// Parent is at depth=2 (already at the limit). Spawning would land
	// the child at depth 3 which exceeds MaxRecursionDepth=2.
	parent, _ := m.CreatePRD("at-limit spec", "/p", "claude", EffortNormal)
	parent.Depth = 2
	_ = m.Store().SetStories(parent.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "child", Spec: "more", SpawnPRD: true}},
	}})
	parent, _ = m.Store().GetPRD(parent.ID)
	parent.Depth = 2 // SetStories may have overwritten — re-set
	parent.Status = PRDApproved
	_ = m.Store().SavePRD(parent)

	_ = m.Run(context.Background(), parent.ID, fakeSpawn, fakeVerify)

	parent, _ = m.Store().GetPRD(parent.ID)
	pt := parent.Story[0].Tasks[0]
	if pt.Status != TaskFailed {
		t.Fatalf("parent task status = %s, want failed", pt.Status)
	}
	if !strings.Contains(pt.Error, "max_recursion_depth") {
		t.Fatalf("error didn't mention depth: %q", pt.Error)
	}
}

func TestRecurse_DepthZero_DisablesRecursion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRecursionDepth = 0
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)

	parent, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(parent.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "child", Spec: "more", SpawnPRD: true}},
	}})
	parent, _ = m.Store().GetPRD(parent.ID)
	parent.Status = PRDApproved
	_ = m.Store().SavePRD(parent)

	_ = m.Run(context.Background(), parent.ID, fakeSpawn, fakeVerify)
	parent, _ = m.Store().GetPRD(parent.ID)
	pt := parent.Story[0].Tasks[0]
	if pt.Status != TaskFailed {
		t.Fatalf("parent task status = %s, want failed", pt.Status)
	}
	if !strings.Contains(pt.Error, "recursion disabled") {
		t.Fatalf("error didn't mention disabled: %q", pt.Error)
	}
}

func TestRecurse_AutoApproveOff_BlocksParent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRecursionDepth = 3
	cfg.AutoApproveChildren = false
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)

	parent, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(parent.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "child", Spec: "more", SpawnPRD: true}},
	}})
	parent, _ = m.Store().GetPRD(parent.ID)
	parent.Status = PRDApproved
	_ = m.Store().SavePRD(parent)

	_ = m.Run(context.Background(), parent.ID, fakeSpawn, fakeVerify)
	parent, _ = m.Store().GetPRD(parent.ID)
	pt := parent.Story[0].Tasks[0]
	if pt.Status != TaskBlocked {
		t.Fatalf("parent task status = %s, want blocked", pt.Status)
	}
	if pt.ChildPRDID == "" {
		t.Fatalf("ChildPRDID empty — child wasn't created before block")
	}
	child, _ := m.Store().GetPRD(pt.ChildPRDID)
	if child.Status != PRDNeedsReview {
		t.Fatalf("child status = %s, want needs_review (auto-approve was off)", child.Status)
	}
}

func TestStore_ListChildPRDs_SortsByCreatedAt(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	parent, _ := st.CreatePRD("parent", "/p", "claude", EffortNormal)
	c1, _ := st.CreatePRDWithParent("c1", "/p", "claude", EffortNormal, parent.ID, "task1", 1)
	c2, _ := st.CreatePRDWithParent("c2", "/p", "claude", EffortNormal, parent.ID, "task2", 1)
	_, _ = st.CreatePRDWithParent("orphan", "/p", "claude", EffortNormal, "different-parent", "", 1)

	kids := st.ListChildPRDs(parent.ID)
	if len(kids) != 2 {
		t.Fatalf("ListChildPRDs returned %d, want 2", len(kids))
	}
	if kids[0].ID != c1.ID || kids[1].ID != c2.ID {
		t.Fatalf("child order wrong: got %s,%s want %s,%s", kids[0].ID, kids[1].ID, c1.ID, c2.ID)
	}
}
