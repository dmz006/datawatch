// v5.19.0 — DeletePRD + EditPRDFields tests. Operator-blocking gap:
// pre-v5.19.0 the autonomous PRD list had no full-CRUD affordance —
// Cancel only flipped status, no hard-delete, no PRD-level edit.

package autonomous

import (
	"strings"
	"testing"
)

func TestStore_DeletePRD_RemovesFromMap(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	prd, _ := st.CreatePRD("delete me", "/p", "claude", EffortNormal)

	if err := st.DeletePRD(prd.ID); err != nil {
		t.Fatalf("DeletePRD: %v", err)
	}
	if _, ok := st.GetPRD(prd.ID); ok {
		t.Fatal("PRD still in map after DeletePRD")
	}
}

func TestStore_DeletePRD_RemovesDescendants(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	parent, _ := st.CreatePRD("parent", "/p", "claude", EffortNormal)
	child, _ := st.CreatePRDWithParent("child", "/p", "claude", EffortNormal, parent.ID, "task1", 1)
	gchild, _ := st.CreatePRDWithParent("grandchild", "/p", "claude", EffortNormal, child.ID, "taskN", 2)
	unrelated, _ := st.CreatePRD("unrelated", "/p", "claude", EffortNormal)

	if err := st.DeletePRD(parent.ID); err != nil {
		t.Fatalf("DeletePRD: %v", err)
	}
	for _, id := range []string{parent.ID, child.ID, gchild.ID} {
		if _, ok := st.GetPRD(id); ok {
			t.Errorf("PRD %s still present after recursive delete", id)
		}
	}
	if _, ok := st.GetPRD(unrelated.ID); !ok {
		t.Error("unrelated PRD got deleted — recursive walk overshot")
	}
}

func TestStore_DeletePRD_NotFoundIsError(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	if err := st.DeletePRD("does-not-exist"); err == nil {
		t.Fatal("expected error for missing PRD")
	}
}

func TestManager_DeletePRD_RefusesRunning(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("running", "/p", "claude", EffortNormal)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)

	err := m.DeletePRD(prd.ID)
	if err == nil {
		t.Fatal("expected refusal when PRD is running")
	}
	if !strings.Contains(err.Error(), "running") {
		t.Fatalf("error didn't mention running: %v", err)
	}
}

func TestStore_UpdatePRDFields_Title(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	prd, _ := st.CreatePRD("original spec", "/p", "claude", EffortNormal)

	updated, err := st.UpdatePRDFields(prd.ID, "New Title", "")
	if err != nil {
		t.Fatalf("UpdatePRDFields: %v", err)
	}
	if updated.Title != "New Title" {
		t.Errorf("title = %q", updated.Title)
	}
	if updated.Spec != "original spec" {
		t.Errorf("spec changed unexpectedly: %q", updated.Spec)
	}
}

func TestStore_UpdatePRDFields_Spec(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	prd, _ := st.CreatePRD("s1", "/p", "claude", EffortNormal)
	prd.Title = "T1"
	_ = st.SavePRD(prd)

	updated, err := st.UpdatePRDFields(prd.ID, "", "rewritten spec")
	if err != nil {
		t.Fatalf("UpdatePRDFields: %v", err)
	}
	if updated.Title != "T1" {
		t.Errorf("title changed: %q", updated.Title)
	}
	if updated.Spec != "rewritten spec" {
		t.Errorf("spec = %q", updated.Spec)
	}
}

func TestStore_UpdatePRDFields_RefusesRunning(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	prd, _ := st.CreatePRD("s1", "/p", "claude", EffortNormal)
	prd.Status = PRDRunning
	_ = st.SavePRD(prd)

	if _, err := st.UpdatePRDFields(prd.ID, "X", ""); err == nil {
		t.Fatal("expected refusal when PRD is running")
	}
}

func TestManager_EditPRDFields_AppendsDecision(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("orig", "/p", "claude", EffortNormal)

	beforeCount := len(prd.Decisions)
	updPRD, err := m.EditPRDFields(prd.ID, "Edited Title", "edited spec", "alice")
	if err != nil {
		t.Fatalf("EditPRDFields: %v", err)
	}
	if updPRD == nil {
		t.Fatal("EditPRDFields returned nil PRD")
	}
	if updPRD.Title != "Edited Title" || updPRD.Spec != "edited spec" {
		t.Errorf("fields not applied: %+v", updPRD)
	}
	if len(updPRD.Decisions) != beforeCount+1 {
		t.Errorf("decision not appended: count = %d", len(updPRD.Decisions))
	}
	last := updPRD.Decisions[len(updPRD.Decisions)-1]
	if last.Kind != "edit" {
		t.Errorf("decision kind = %q, want edit", last.Kind)
	}
	if last.Actor != "alice" {
		t.Errorf("decision actor = %q, want alice", last.Actor)
	}
}
